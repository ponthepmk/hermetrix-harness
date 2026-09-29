package product

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"hermetrix-harness/internal/identity"
)

type sampledVideoFrame struct {
	ref    string
	size   int
	width  int
	height int
	second float64
}

// runVideoExtract records three bounded visual samples with exact timestamps.
// It makes no claim about motion between samples or about the audio track.
func (s *Service) runVideoExtract(ctx context.Context, jobID string) {
	defer func() { s.mu.Lock(); delete(s.cancels, jobID); s.mu.Unlock() }()
	select {
	case s.mediaSem <- struct{}{}:
		defer func() { <-s.mediaSem }()
	case <-ctx.Done():
		s.finishMediaContext(jobID, ctx.Err(), "job stopped before frame extraction")
		return
	}
	now := time.Now().UTC()
	changed, err := s.store.DB.ExecContext(context.Background(), `UPDATE media_jobs SET state='running',progress=.05,attempt_count=attempt_count+1,started_at=?,updated_at=? WHERE id=? AND state='queued' AND cancel_requested=0`, formatTime(now), formatTime(now), jobID)
	if err != nil {
		return
	}
	if count, _ := changed.RowsAffected(); count != 1 {
		return
	}
	job, err := s.getMediaJob(context.Background(), jobID, "")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "store_error", err.Error())
		return
	}
	artifact, file, err := s.OpenVideoArtifact(context.Background(), job.SourceArtifactID)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "source_unavailable", err.Error())
		return
	}
	defer file.Close()
	if artifact.Checksum != job.SourceHash {
		s.finishMediaFailure(jobID, "failed", "stale_source", "video checksum changed")
		return
	}
	duration, ok := artifact.Metadata["duration_seconds"].(float64)
	if !ok || duration <= 0 || duration > 600 {
		s.finishMediaFailure(jobID, "failed", "invalid_video", "validated duration is unavailable")
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "processor_unavailable", "ffmpeg is not installed")
		return
	}
	times := []float64{0, duration / 2, duration - 0.1}
	if times[2] < 0 {
		times[2] = 0
	}
	frames := make([]sampledVideoFrame, 0, len(times))
	for index, second := range times {
		if ctx.Err() != nil {
			s.finishMediaContext(jobID, ctx.Err(), "job stopped during frame extraction")
			return
		}
		temp, err := os.CreateTemp(s.store.Root, ".video-frame-*.png")
		if err != nil {
			s.finishMediaFailure(jobID, "failed", "temp_unavailable", err.Error())
			return
		}
		path := temp.Name()
		temp.Close()
		args := []string{"-nostdin", "-v", "error", "-threads", "2", "-protocol_whitelist", "file", "-ss", strconv.FormatFloat(second, 'f', 3, 64), "-i", file.Name(), "-frames:v", "1", "-an", "-vf", "scale=1280:720:force_original_aspect_ratio=decrease", "-y", path}
		output, runErr := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
		if runErr != nil {
			os.Remove(path)
			if ctx.Err() != nil {
				s.finishMediaContext(jobID, ctx.Err(), "job stopped during frame extraction")
			} else {
				if len(output) > 240 {
					output = output[:240]
				}
				s.finishMediaFailure(jobID, "failed", "decode_failed", fmt.Sprintf("frame at %.2fs: %v %s", second, runErr, output))
			}
			return
		}
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() < 1 || info.Size() > maxMediaImageBytes {
			os.Remove(path)
			s.finishMediaFailure(jobID, "failed", "resource_limit", "decoded frame exceeded the 8 MiB limit")
			return
		}
		data, readErr := os.ReadFile(path)
		os.Remove(path)
		if readErr != nil {
			s.finishMediaFailure(jobID, "failed", "decode_failed", readErr.Error())
			return
		}
		width, height, mimeType, inspectErr := inspectImage(data)
		if inspectErr != nil || mimeType != "image/png" || int64(width)*int64(height) > maxMediaImagePixels {
			s.finishMediaFailure(jobID, "failed", "decode_failed", "frame PNG failed validation")
			return
		}
		ref, putErr := s.store.Blobs.Put(data)
		if putErr != nil {
			s.finishMediaFailure(jobID, "failed", "cas_write_failed", putErr.Error())
			return
		}
		frames = append(frames, sampledVideoFrame{ref: ref, size: len(data), width: width, height: height, second: second})
		_, _ = s.store.DB.ExecContext(context.Background(), `UPDATE media_jobs SET progress=?,updated_at=? WHERE id=? AND state='running'`, .1+float64(index+1)*.25, formatTime(time.Now().UTC()), jobID)
	}
	if ctx.Err() != nil {
		s.finishMediaContext(jobID, ctx.Err(), "job stopped before saving frames")
		return
	}
	created := time.Now().UTC()
	tx, err := s.store.DB.BeginTx(context.Background(), nil)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "store_error", err.Error())
		return
	}
	defer tx.Rollback()
	ids := []string{}
	for index, frame := range frames {
		id := identity.New("artifact")
		metadata, _ := json.Marshal(map[string]any{"media_job_id": job.ID, "source_artifact_id": artifact.ID, "source_hash": artifact.Checksum,
			"processor_revision": job.ProcessorRevision, "frame_time_seconds": frame.second, "sample_index": index,
			"width": frame.width, "height": frame.height, "inspection": "sampled frame only; audio and motion were not analyzed"})
		name := fmt.Sprintf("%s.frame-%02d-%05.2fs.png", filepath.Base(artifact.Name), index+1, frame.second)
		_, err = tx.Exec(`INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id,source_lineage_json)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, nullIfEmpty(artifact.ProjectID), name, "video_frame", "image/png", frame.ref, frame.size, frame.ref, string(metadata), formatTime(created), job.OwnerPrincipalID, fmt.Sprintf("[%q]", artifact.ID))
		if err != nil {
			break
		}
		_, err = tx.Exec(`INSERT INTO artifact_derivations(id,owner_principal_id,source_artifact_id,result_artifact_id,relation_kind,source_hash,result_hash,metadata_json,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("derivation"), job.OwnerPrincipalID, artifact.ID, id, "video_extract", artifact.Checksum, frame.ref, string(metadata), formatTime(created))
		if err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		encoded, _ := json.Marshal(ids)
		result, updateErr := tx.Exec(`UPDATE media_jobs SET state='completed',progress=1,result_artifact_ids_json=?,updated_at=?,completed_at=? WHERE id=? AND state='running' AND cancel_requested=0`, string(encoded), formatTime(created), formatTime(created), job.ID)
		err = updateErr
		if err == nil {
			count, _ := result.RowsAffected()
			if count != 1 {
				err = fmt.Errorf("media job was cancelled before commit")
			}
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if ctx.Err() != nil {
			s.finishMediaContext(jobID, ctx.Err(), "job stopped before saving frames")
		} else {
			s.finishMediaFailure(jobID, "failed", "commit_failed", err.Error())
		}
	}
}
