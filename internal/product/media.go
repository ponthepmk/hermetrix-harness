package product

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

const (
	maxMediaImageBytes  = 8 << 20
	maxMediaImagePixels = 24_000_000
)

var (
	ErrMediaProcessorUnsupported = errors.New("media processor unsupported")
	ErrIdempotencyConflict       = errors.New("idempotency key payload conflict")
)

func (s *Service) UploadMedia(ctx context.Context, input MediaUploadInput) (Artifact, error) {
	if len(input.Data) == 0 || len(input.Data) > maxMediaImageBytes {
		return Artifact{}, fmt.Errorf("image upload must be between 1 byte and 8 MiB")
	}
	width, height, mimeType, err := inspectImage(input.Data)
	if err != nil || width < 1 || height < 1 || int64(width)*int64(height) > maxMediaImagePixels {
		return Artifact{}, fmt.Errorf("image upload failed decoded MIME, dimension, or animation validation")
	}
	if declared := strings.TrimSpace(strings.ToLower(input.MIMEType)); declared != "" && declared != mimeType {
		return Artifact{}, fmt.Errorf("declared media type does not match decoded content")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "media-upload" + imageExtension(mimeType)
	}
	return s.CreateArtifact(ctx, ArtifactInput{ProjectID: input.ProjectID, SessionID: input.SessionID, Name: name,
		Kind: "image", MIMEType: mimeType, Content: string(input.Data), Metadata: map[string]any{
			"source": "media-upload", "width": width, "height": height, "validation": "decoded-v1"}})
}

func imageExtension(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func inspectImage(data []byte) (int, int, string, error) {
	if len(data) < 12 {
		return 0, 0, "", fmt.Errorf("truncated image")
	}
	detected := strings.Split(http.DetectContentType(data[:min(len(data), 512)]), ";")[0]
	if detected == "image/png" {
		if bytes.Contains(data, []byte("acTL")) {
			return 0, 0, detected, fmt.Errorf("animated PNG is unsupported")
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, detected, err
	}
	if detected == "image/jpeg" {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		return cfg.Width, cfg.Height, detected, err
	}
	if detected != "image/webp" || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, detected, fmt.Errorf("only JPEG, PNG and WebP are supported")
	}
	if len(data) < 30 {
		return 0, 0, detected, fmt.Errorf("truncated WebP")
	}
	switch string(data[12:16]) {
	case "VP8X":
		if data[20]&0x02 != 0 {
			return 0, 0, detected, fmt.Errorf("animated WebP is unsupported")
		}
		return 1 + int(data[24]) + (int(data[25]) << 8) + (int(data[26]) << 16), 1 + int(data[27]) + (int(data[28]) << 8) + (int(data[29]) << 16), detected, nil
	case "VP8 ":
		if data[23] != 0x9d || data[24] != 0x01 || data[25] != 0x2a {
			return 0, 0, detected, fmt.Errorf("invalid WebP")
		}
		return int(binary.LittleEndian.Uint16(data[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(data[28:30]) & 0x3fff), detected, nil
	case "VP8L":
		if len(data) < 25 || data[20] != 0x2f {
			return 0, 0, detected, fmt.Errorf("invalid WebP")
		}
		return 1 + int(data[21]) + (int(data[22]&0x3f) << 8), 1 + int(data[22]>>6) + (int(data[23]) << 2) + (int(data[24]&0x0f) << 10), detected, nil
	}
	return 0, 0, detected, fmt.Errorf("unsupported WebP encoding")
}

func (s *Service) StartMediaJob(ctx context.Context, input MediaJobInput) (MediaJob, error) {
	input.SourceArtifactID, input.ProcessorKind = strings.TrimSpace(input.SourceArtifactID), strings.TrimSpace(input.ProcessorKind)
	input.OperationID, input.IdempotencyKey = strings.TrimSpace(input.OperationID), strings.TrimSpace(input.IdempotencyKey)
	if input.SourceArtifactID == "" || input.ProcessorKind == "" || input.OperationID == "" || input.IdempotencyKey == "" {
		return MediaJob{}, fmt.Errorf("source, processor, operation_id and idempotency_key are required")
	}
	if len(input.IdempotencyKey) > 128 || len(input.OperationID) > 128 {
		return MediaJob{}, fmt.Errorf("operation and idempotency keys must be at most 128 bytes")
	}
	if input.ProcessorKind != "image_inspect" {
		return MediaJob{}, fmt.Errorf("%w: %s is disabled until a contained FFmpeg/Whisper runtime is qualified", ErrMediaProcessorUnsupported, input.ProcessorKind)
	}
	artifact, data, err := s.GetArtifact(ctx, input.SourceArtifactID)
	if err != nil {
		return MediaJob{}, err
	}
	if artifact.Kind != "image" || len(data) > maxMediaImageBytes {
		return MediaJob{}, fmt.Errorf("image_inspect requires a validated image artifact")
	}
	if _, _, detected, imageErr := inspectImage(data); imageErr != nil || detected != artifact.MIMEType {
		return MediaJob{}, fmt.Errorf("source image failed decoded MIME validation")
	}
	if input.ProcessorRevision == "" {
		input.ProcessorRevision = "builtin-image-inspect-v1"
	}
	settings, _ := json.Marshal(input.Options)
	settingsSum := sha256.Sum256(settings)
	payload, _ := json.Marshal(struct{ Source, Hash, Kind, Processor, Model, Settings string }{
		artifact.ID, artifact.Checksum, input.ProcessorKind, input.ProcessorRevision, input.ModelRevision, hex.EncodeToString(settingsSum[:])})
	payloadSum := sha256.Sum256(payload)
	payloadHash, settingsDigest := hex.EncodeToString(payloadSum[:]), hex.EncodeToString(settingsSum[:])
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return MediaJob{}, err
	}
	if existing, existingErr := s.mediaJobByKey(ctx, ownerID, input.OperationID, input.IdempotencyKey); existingErr == nil {
		if existing.PayloadHash != payloadHash {
			return MediaJob{}, ErrIdempotencyConflict
		}
		return existing, nil
	} else if !errors.Is(existingErr, sql.ErrNoRows) {
		return MediaJob{}, existingErr
	}
	now := time.Now().UTC()
	job := MediaJob{ID: identity.New("mediajob"), OwnerPrincipalID: ownerID, SourceArtifactID: artifact.ID, SourceHash: artifact.Checksum,
		ProcessorKind: input.ProcessorKind, ProcessorRevision: input.ProcessorRevision, ModelRevision: input.ModelRevision,
		SettingsDigest: settingsDigest, OperationID: input.OperationID, IdempotencyKey: input.IdempotencyKey, PayloadHash: payloadHash,
		State: "queued", ResultArtifactIDs: []string{}, CreatedAt: now, UpdatedAt: now}
	_, err = s.store.DB.ExecContext(ctx, `INSERT INTO media_jobs(id,owner_principal_id,source_artifact_id,source_hash,processor_kind,
		processor_revision,model_revision,settings_digest,operation_id,idempotency_key,payload_hash,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,'queued',?,?)`, job.ID, ownerID, artifact.ID, artifact.Checksum, job.ProcessorKind,
		job.ProcessorRevision, job.ModelRevision, job.SettingsDigest, job.OperationID, job.IdempotencyKey, job.PayloadHash, formatTime(now), formatTime(now))
	if err != nil {
		return MediaJob{}, err
	}
	jobCtx, cancel := context.WithTimeout(s.teamCtx, 120*time.Second)
	s.mu.Lock()
	s.cancels[job.ID] = cancel
	s.mu.Unlock()
	s.teamWG.Add(1)
	go func() { defer s.teamWG.Done(); s.runImageInspect(jobCtx, job.ID) }()
	return job, nil
}

func (s *Service) runImageInspect(ctx context.Context, jobID string) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
	}()
	select {
	case s.mediaSem <- struct{}{}:
		defer func() { <-s.mediaSem }()
	case <-ctx.Done():
		s.finishMediaContext(jobID, ctx.Err(), "job stopped before decode")
		return
	}
	now := time.Now().UTC()
	result, err := s.store.DB.ExecContext(context.Background(), `UPDATE media_jobs SET state='running',progress=.1,attempt_count=attempt_count+1,
		started_at=?,updated_at=? WHERE id=? AND state='queued' AND cancel_requested=0`, formatTime(now), formatTime(now), jobID)
	if err != nil {
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		job, getErr := s.getMediaJob(context.Background(), jobID, "")
		if getErr == nil && job.CancelRequested {
			s.finishMediaFailure(jobID, "cancelled", "cancelled", "job cancelled")
		}
		return
	}
	job, err := s.getMediaJob(context.Background(), jobID, "")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "store_error", err.Error())
		return
	}
	artifact, data, err := s.GetArtifact(context.Background(), job.SourceArtifactID)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "source_unavailable", err.Error())
		return
	}
	width, height, mimeType, err := inspectImage(data)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "decode_failed", err.Error())
		return
	}
	select {
	case <-ctx.Done():
		s.finishMediaContext(jobID, ctx.Err(), "job stopped during decode")
		return
	default:
	}
	summary := map[string]any{"schema": "hermetrix.image-evidence.v1", "source_artifact_id": artifact.ID,
		"source_hash": artifact.Checksum, "mime_type": mimeType, "width": width, "height": height,
		"anchors":     []map[string]any{{"kind": "full_image", "x": 0, "y": 0, "width": width, "height": height}},
		"limitations": []string{"metadata and dimensions only; no OCR or semantic vision inference was performed"}}
	body, _ := json.Marshal(summary)
	ref, err := s.store.Blobs.Put(body)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "cas_write_failed", err.Error())
		return
	}
	ownerID := job.OwnerPrincipalID
	derivedID, created := identity.New("artifact"), time.Now().UTC()
	tx, err := s.store.DB.BeginTx(context.Background(), nil)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "store_error", err.Error())
		return
	}
	defer tx.Rollback()
	metadata, _ := json.Marshal(map[string]any{"media_job_id": job.ID, "source_artifact_id": artifact.ID, "processor_revision": job.ProcessorRevision})
	_, err = tx.Exec(`INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,
		owner_principal_id,source_lineage_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, derivedID, nullIfEmpty(artifact.ProjectID),
		filepath.Base(artifact.Name)+".evidence.json", "image_evidence", "application/vnd.hermetrix.image-evidence+json", ref,
		len(body), ref, string(metadata), formatTime(created), ownerID, fmt.Sprintf("[%q]", artifact.ID))
	if err == nil {
		_, err = tx.Exec(`INSERT INTO artifact_derivations(id,owner_principal_id,source_artifact_id,result_artifact_id,relation_kind,
			source_hash,result_hash,metadata_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("derivation"), ownerID,
			artifact.ID, derivedID, "image_inspect", artifact.Checksum, ref, string(metadata), formatTime(created))
	}
	resultJSON, _ := json.Marshal([]string{derivedID})
	if err == nil {
		_, err = tx.Exec(`UPDATE media_jobs SET state='completed',progress=1,result_artifact_ids_json=?,updated_at=?,completed_at=?
			WHERE id=? AND state='running' AND cancel_requested=0`, string(resultJSON), formatTime(created), formatTime(created), job.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "commit_failed", err.Error())
	}
}

func (s *Service) finishMediaContext(id string, cause error, message string) {
	if errors.Is(cause, context.DeadlineExceeded) {
		s.finishMediaFailure(id, "failed", "resource_limit", message+": 120 second wall deadline exceeded")
		return
	}
	s.finishMediaFailure(id, "cancelled", "cancelled", message)
}

func (s *Service) finishMediaFailure(id, state, code, message string) {
	now := formatTime(time.Now().UTC())
	_, _ = s.store.DB.Exec(`UPDATE media_jobs SET state=?,error_code=?,error=?,updated_at=?,completed_at=?
		WHERE id=? AND state IN ('queued','running')`, state, code, message, now, now, id)
	s.mu.Lock()
	delete(s.cancels, id)
	s.mu.Unlock()
}

func (s *Service) CancelMediaJob(ctx context.Context, id string) (MediaJob, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return MediaJob{}, err
	}
	now := formatTime(time.Now().UTC())
	_, err = s.store.DB.ExecContext(ctx, `UPDATE media_jobs SET cancel_requested=1,
		state=CASE WHEN state='queued' THEN 'cancelled' ELSE state END,
		error_code=CASE WHEN state='queued' THEN 'cancelled' ELSE error_code END,
		error=CASE WHEN state='queued' THEN 'job cancelled' ELSE error END,updated_at=?,
		completed_at=CASE WHEN state='queued' THEN ? ELSE completed_at END WHERE id=? AND owner_principal_id=?
		AND state IN ('queued','running')`, now, now, id, ownerID)
	if err != nil {
		return MediaJob{}, err
	}
	s.mu.Lock()
	cancel := s.cancels[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.getMediaJob(ctx, id, ownerID)
}

func (s *Service) GetMediaJob(ctx context.Context, id string) (MediaJob, error) {
	ownerID, err := s.store.OwnerPrincipalID(ctx)
	if err != nil {
		return MediaJob{}, err
	}
	return s.getMediaJob(ctx, id, ownerID)
}

func (s *Service) mediaJobByKey(ctx context.Context, ownerID, operationID, key string) (MediaJob, error) {
	var id string
	err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM media_jobs WHERE owner_principal_id=? AND operation_id=? AND idempotency_key=?`,
		ownerID, operationID, key).Scan(&id)
	if err != nil {
		return MediaJob{}, err
	}
	return s.getMediaJob(ctx, id, ownerID)
}

func (s *Service) getMediaJob(ctx context.Context, id, ownerID string) (MediaJob, error) {
	query := `SELECT id,owner_principal_id,source_artifact_id,source_hash,processor_kind,processor_revision,model_revision,
		settings_digest,operation_id,idempotency_key,payload_hash,state,progress,attempt_count,result_artifact_ids_json,error_code,error,
		cancel_requested,created_at,started_at,updated_at,completed_at FROM media_jobs WHERE id=?`
	args := []any{id}
	if ownerID != "" {
		query += ` AND owner_principal_id=?`
		args = append(args, ownerID)
	}
	var item MediaJob
	var results, created, updated string
	var started, completed sql.NullString
	err := s.store.DB.QueryRowContext(ctx, query, args...).Scan(&item.ID, &item.OwnerPrincipalID, &item.SourceArtifactID, &item.SourceHash,
		&item.ProcessorKind, &item.ProcessorRevision, &item.ModelRevision, &item.SettingsDigest, &item.OperationID, &item.IdempotencyKey,
		&item.PayloadHash, &item.State, &item.Progress, &item.AttemptCount, &results, &item.ErrorCode, &item.Error,
		&item.CancelRequested, &created, &started, &updated, &completed)
	if err != nil {
		return item, err
	}
	_ = json.Unmarshal([]byte(results), &item.ResultArtifactIDs)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if started.Valid {
		value, _ := time.Parse(time.RFC3339Nano, started.String)
		item.StartedAt = &value
	}
	if completed.Valid {
		value, _ := time.Parse(time.RFC3339Nano, completed.String)
		item.CompletedAt = &value
	}
	return item, nil
}
