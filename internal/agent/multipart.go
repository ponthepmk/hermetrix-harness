package agent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/providers"
)

const maxImageBytes = 8 << 20
const maxImagePixels = 24_000_000

func (s *Service) validateTurnParts(ctx context.Context, session Session, input []EventPart) ([]EventPart, string, error) {
	if len(input) == 0 || len(input) > 32 {
		return nil, "", fmt.Errorf("multipart turn requires 1 to 32 parts")
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, "", err
	}
	parts := make([]EventPart, 0, len(input))
	textBytes, images := 0, 0
	goal := make([]string, 0, len(input))
	for ordinal, source := range input {
		part := EventPart{ID: identity.New("part"), Ordinal: ordinal, Kind: strings.TrimSpace(source.Kind),
			Text: source.Text, ArtifactID: strings.TrimSpace(source.ArtifactID), Metadata: source.Metadata}
		if part.Metadata == nil {
			part.Metadata = map[string]any{}
		}
		metadata, _ := json.Marshal(part.Metadata)
		if len(metadata) > 16<<10 {
			return nil, "", fmt.Errorf("part metadata exceeds 16 KiB")
		}
		switch part.Kind {
		case "text":
			part.Text = strings.TrimSpace(part.Text)
			if part.Text == "" || part.ArtifactID != "" {
				return nil, "", fmt.Errorf("text part requires text only")
			}
			textBytes += len(part.Text)
			goal = append(goal, part.Text)
		case "image":
			images++
			if part.ArtifactID == "" || strings.TrimSpace(part.Text) != "" {
				return nil, "", fmt.Errorf("image part requires one artifact reference")
			}
			var mimeType, blobRef, artifactOwner, projectID, artifactSession string
			var byteSize int
			err = s.store.DB.QueryRowContext(ctx, `SELECT mime_type,blob_ref,owner_principal_id,COALESCE(project_id,''),COALESCE(session_id,''),byte_size
				FROM artifacts WHERE id=?`, part.ArtifactID).Scan(&mimeType, &blobRef, &artifactOwner, &projectID, &artifactSession, &byteSize)
			if err != nil || artifactOwner != ownerID || (projectID != session.ProjectID && artifactSession != session.ID) {
				return nil, "", fmt.Errorf("image artifact is unavailable to this session")
			}
			if byteSize < 1 || byteSize > maxImageBytes {
				return nil, "", fmt.Errorf("image artifact exceeds 8 MiB")
			}
			data, readErr := s.store.Blobs.Get(blobRef)
			if readErr != nil {
				return nil, "", readErr
			}
			width, height, detected, imageErr := boundedImageInfo(data)
			if imageErr != nil || detected != mimeType || width*height > maxImagePixels {
				return nil, "", fmt.Errorf("image artifact has invalid MIME, dimensions, or animation")
			}
			part.Metadata["mime_type"], part.Metadata["width"], part.Metadata["height"] = mimeType, width, height
			goal = append(goal, "[image:"+part.ArtifactID+"]")
		default:
			return nil, "", fmt.Errorf("unsupported multipart kind %q", part.Kind)
		}
		parts = append(parts, part)
	}
	if textBytes > maxUserMessage || images > 5 {
		return nil, "", fmt.Errorf("multipart text or image count exceeds request limits")
	}
	return parts, strings.Join(goal, "\n"), nil
}

func (s *Service) materializeTurnParts(ctx context.Context, sessionID, turnID string) ([]providers.ContentPart, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT p.kind,COALESCE(p.text_content,''),COALESCE(p.artifact_id,''),
		COALESCE(a.mime_type,''),COALESCE(a.blob_ref,''),COALESCE(a.checksum,''),COALESCE(a.owner_principal_id,'')
		FROM agent_event_parts p JOIN agent_events e ON e.id=p.event_id
		LEFT JOIN artifacts a ON a.id=p.artifact_id
		JOIN agent_sessions s ON s.id=e.session_id
		WHERE e.session_id=? AND e.turn_id=? AND e.role='user' AND s.owner_principal_id=? ORDER BY p.ordinal`, sessionID, turnID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := []providers.ContentPart{}
	for rows.Next() {
		var kind, text, artifactID, mimeType, blobRef, checksum, artifactOwner string
		if err = rows.Scan(&kind, &text, &artifactID, &mimeType, &blobRef, &checksum, &artifactOwner); err != nil {
			return nil, err
		}
		part := providers.ContentPart{Kind: kind, Text: text, ArtifactID: artifactID, MediaType: mimeType, Checksum: checksum}
		if kind == "image" {
			if artifactOwner != ownerID {
				return nil, fmt.Errorf("multipart artifact ownership changed")
			}
			part.Data, err = s.store.Blobs.Get(blobRef)
			if err != nil {
				return nil, err
			}
		}
		parts = append(parts, part)
	}
	return parts, rows.Err()
}

func loadEventParts(ctx context.Context, db *sql.DB, eventID string) ([]EventPart, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,ordinal,kind,COALESCE(text_content,''),COALESCE(artifact_id,''),metadata_json
		FROM agent_event_parts WHERE event_id=? ORDER BY ordinal`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	parts := []EventPart{}
	for rows.Next() {
		var item EventPart
		var metadata string
		if err = rows.Scan(&item.ID, &item.Ordinal, &item.Kind, &item.Text, &item.ArtifactID, &metadata); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metadata), &item.Metadata)
		parts = append(parts, item)
	}
	return parts, rows.Err()
}

func boundedImageInfo(data []byte) (int, int, string, error) {
	if len(data) == 0 {
		return 0, 0, "", fmt.Errorf("empty image")
	}
	detected := strings.Split(http.DetectContentType(data[:min(len(data), 512)]), ";")[0]
	if detected == "image/jpeg" || detected == "image/png" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, detected, err
	}
	if detected != "image/webp" || len(data) < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, detected, fmt.Errorf("unsupported image")
	}
	switch string(data[12:16]) {
	case "VP8X":
		if data[20]&0x02 != 0 {
			return 0, 0, detected, fmt.Errorf("animated webp is unsupported")
		}
		return 1 + int(data[24]) + (int(data[25]) << 8) + (int(data[26]) << 16), 1 + int(data[27]) + (int(data[28]) << 8) + (int(data[29]) << 16), detected, nil
	case "VP8 ":
		if len(data) < 30 || data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, detected, fmt.Errorf("invalid webp")
		}
		return int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff), detected, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0, detected, fmt.Errorf("invalid webp")
		}
		return 1 + int(data[21]) + (int(data[22]&0x3f) << 8), 1 + int(data[22]>>6) + (int(data[23]) << 2) + (int(data[24]&0x0f) << 10), detected, nil
	}
	return 0, 0, detected, fmt.Errorf("unsupported webp encoding")
}
