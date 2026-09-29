package product

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const maxVideoBytes int64 = 256 << 20

type VideoUploadInput struct {
	ProjectID string
	SessionID string
	Name      string
	Reader    io.Reader
}

type videoProbe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func (s *Service) UploadVideo(ctx context.Context, input VideoUploadInput) (Artifact, error) {
	if input.Reader == nil {
		return Artifact{}, fmt.Errorf("video file is required")
	}
	if input.ProjectID != "" {
		if _, err := s.GetProject(ctx, input.ProjectID); err != nil {
			return Artifact{}, err
		}
	}
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return Artifact{}, err
	}
	if input.SessionID != "" {
		var sessionOwner string
		if err := s.store.DB.QueryRowContext(ctx, `SELECT owner_principal_id FROM agent_sessions WHERE id=?`, input.SessionID).Scan(&sessionOwner); err != nil || sessionOwner != ownerID {
			return Artifact{}, fmt.Errorf("video session is unavailable to this principal")
		}
	}
	ref, size, err := s.store.Blobs.PutReader(input.Reader, maxVideoBytes)
	if err != nil {
		return Artifact{}, err
	}
	file, err := s.store.Blobs.OpenVerified(ref)
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	mimeType, metadata, err := probeVideo(ctx, file)
	if err != nil {
		return Artifact{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "video-upload"
	}
	name = strings.ReplaceAll(strings.ReplaceAll(name, "\r", ""), "\n", "")
	if len(name) > 240 {
		name = name[:240]
	}
	now := time.Now().UTC()
	item := Artifact{ID: identity.New("artifact"), ProjectID: input.ProjectID, SessionID: input.SessionID,
		Name: name, Kind: "video", MIMEType: mimeType, BlobRef: ref, ByteSize: int(size), Checksum: ref,
		Metadata: metadata, CreatedAt: now, OwnerPrincipalID: ownerID, Visibility: "private", ExportPolicy: "deny", SharingRevision: 1}
	metadataJSON, _ := json.Marshal(metadata)
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,session_id,name,kind,mime_type,blob_ref,
		byte_size,checksum,metadata_json,created_at,owner_principal_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID,
		nullIfEmpty(item.ProjectID), nullIfEmpty(item.SessionID), item.Name, item.Kind, item.MIMEType, item.BlobRef,
		item.ByteSize, item.Checksum, string(metadataJSON), formatTime(now), ownerID)
	return item, err
}

func probeVideo(ctx context.Context, file *os.File) (string, map[string]any, error) {
	header := make([]byte, 4096)
	n, err := file.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return "", nil, err
	}
	header = header[:n]
	mimeType := ""
	if len(header) >= 12 && string(header[4:8]) == "ftyp" {
		mimeType = "video/mp4"
	} else if len(header) >= 4 && string(header[:4]) == "\x1a\x45\xdf\xa3" && strings.Contains(strings.ToLower(string(header)), "webm") {
		mimeType = "video/webm"
	}
	if mimeType == "" {
		return "", nil, fmt.Errorf("only decoded MP4 or WebM video is supported")
	}
	probePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", nil, fmt.Errorf("video inspection is unavailable: ffprobe is not installed")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, probePath, "-v", "error", "-show_entries",
		"format=format_name,duration:stream=codec_type,codec_name,width,height", "-of", "json", file.Name()).Output()
	if err != nil {
		return "", nil, fmt.Errorf("video metadata could not be decoded: %w", err)
	}
	var probe videoProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		return "", nil, fmt.Errorf("video metadata is invalid: %w", err)
	}
	if mimeType == "video/mp4" && !strings.Contains(probe.Format.FormatName, "mp4") && !strings.Contains(probe.Format.FormatName, "mov") {
		return "", nil, fmt.Errorf("MP4 header does not match decoded format")
	}
	if mimeType == "video/webm" && !strings.Contains(probe.Format.FormatName, "webm") {
		return "", nil, fmt.Errorf("WebM header does not match decoded format")
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 600 {
		return "", nil, fmt.Errorf("video duration must be between 0 and 600 seconds")
	}
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" {
			continue
		}
		if stream.Width < 1 || stream.Height < 1 || stream.Width > 3840 || stream.Height > 3840 ||
			int64(stream.Width)*int64(stream.Height) > 3840*2160 {
			return "", nil, fmt.Errorf("video dimensions exceed the 4K limit")
		}
		return mimeType, map[string]any{"source": "video-upload", "validation": "ffprobe-v1", "duration_seconds": duration,
			"width": stream.Width, "height": stream.Height, "video_codec": stream.CodecName,
			"inspection": "container and metadata only; frames and audio were not semantically inspected"}, nil
	}
	return "", nil, fmt.Errorf("video has no decodable video stream")
}

func (s *Service) OpenVideoArtifact(ctx context.Context, id string) (Artifact, *os.File, error) {
	item, err := s.GetArtifactMetadata(ctx, id)
	if err != nil {
		return Artifact{}, nil, err
	}
	if item.Kind != "video" || (item.MIMEType != "video/mp4" && item.MIMEType != "video/webm") {
		return Artifact{}, nil, fmt.Errorf("artifact is not a validated video")
	}
	file, err := s.store.Blobs.OpenVerified(item.BlobRef)
	return item, file, err
}
