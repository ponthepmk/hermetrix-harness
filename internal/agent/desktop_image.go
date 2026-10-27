package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
)

// Only the latest desktop result, when it is a successful human-approved view,
// may contribute
// pixels. Observation/list/mouse receipts never silently upload screen images.
func (s *Service) desktopImageForTurn(ctx context.Context, events []Event, turnID string, profile providers.Profile) (*providers.ContentPart, int, error) {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.TurnID != turnID || event.EventKind != "tool_result" || metadataString(event.Metadata, "tool_name") != "desktop" {
			continue
		}
		var receipt toolruntime.Receipt
		if json.Unmarshal([]byte(event.Content), &receipt) != nil || receipt.Status != "succeeded" {
			return nil, 0, nil
		}
		id := metadataString(receipt.Metadata, "vision_image_artifact_id")
		if id == "" {
			return nil, 0, nil
		}
		if metadataString(receipt.Metadata, "desktop_image_provider_id") != profile.ID {
			return nil, 0, fmt.Errorf("screen image destination changed")
		}
		if err := s.providers.ValidateCapabilities(ctx, profile, "image"); err != nil {
			return nil, 0, err
		}
		owner, err := s.store.OwnerPrincipalID(ctx)
		if err != nil {
			return nil, 0, err
		}
		var mime, ref, checksum, artifactOwner string
		var size int
		err = s.store.DB.QueryRowContext(ctx, `SELECT mime_type,blob_ref,checksum,owner_principal_id,byte_size FROM artifacts WHERE id=?`, id).Scan(&mime, &ref, &checksum, &artifactOwner, &size)
		if err != nil || artifactOwner != owner || size < 1 || size > maxImageBytes {
			return nil, 0, fmt.Errorf("approved screen image is unavailable to this owner")
		}
		data, err := s.store.Blobs.Get(ref)
		if err != nil {
			return nil, 0, err
		}
		w, h, detected, err := boundedImageInfo(data)
		if err != nil || w < 1 || h < 1 || w*h > maxImagePixels || detected != mime {
			return nil, 0, fmt.Errorf("invalid screen image")
		}
		// Conservative reserve using 14px patches plus framing. This is an estimate,
		// not a provider measurement; refuse if the active budget cannot hold it.
		reserve := ((w+13)/14)*((h+13)/14) + 1024
		return &providers.ContentPart{Kind: "image", MediaType: mime, Data: data, ArtifactID: id, Checksum: checksum}, reserve, nil
	}
	return nil, 0, nil
}
