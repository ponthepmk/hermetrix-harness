package product

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hermetrix-harness/internal/durability"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
)

type whisperRuntime struct {
	cliPath     string
	modelPath   string
	modelDigest string
	runtimeDir  string
}

func discoverWhisperRuntime() (whisperRuntime, error) {
	cli := strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_CLI"))
	if cli == "" {
		if found, err := exec.LookPath("whisper-cli"); err == nil {
			cli = found
		} else if runtime.GOOS == "windows" {
			cli = `C:\Tools\whisper.cpp\build\bin\whisper-cli.exe`
		}
	}
	if cli == "" {
		return whisperRuntime{}, fmt.Errorf("video transcription is unavailable: configure HERMETRIX_WHISPER_CLI and HERMETRIX_WHISPER_MODEL")
	}
	if info, err := os.Stat(cli); err != nil || info.IsDir() {
		return whisperRuntime{}, fmt.Errorf("video transcription is unavailable: whisper-cli was not found")
	}
	model := strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_MODEL"))
	if model == "" {
		model = filepath.Clean(filepath.Join(filepath.Dir(cli), "..", "..", "ggml-small.bin"))
	}
	file, err := os.Open(model)
	if err != nil {
		return whisperRuntime{}, fmt.Errorf("video transcription is unavailable: local Whisper model was not found")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() < 1 || info.Size() > 2<<30 {
		return whisperRuntime{}, fmt.Errorf("video transcription is unavailable: local Whisper model is invalid")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return whisperRuntime{}, fmt.Errorf("read local Whisper model: %w", err)
	}
	result := whisperRuntime{cliPath: cli, modelPath: model, modelDigest: hex.EncodeToString(hash.Sum(nil)), runtimeDir: strings.TrimSpace(os.Getenv("HERMETRIX_WHISPER_RUNTIME_DIR"))}
	if result.runtimeDir == "" && runtime.GOOS == "windows" && strings.EqualFold(filepath.Clean(cli), `C:\Tools\whisper.cpp\build\bin\whisper-cli.exe`) {
		result.runtimeDir = `C:\msys64\ucrt64\bin`
	}
	return result, nil
}

func (w whisperRuntime) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, w.cliPath, args...)
	if w.runtimeDir != "" {
		env := os.Environ()
		for i, value := range env {
			if strings.EqualFold(strings.SplitN(value, "=", 2)[0], "PATH") {
				env[i] = value[:strings.IndexByte(value, '=')+1] + w.runtimeDir + string(os.PathListSeparator) + value[strings.IndexByte(value, '=')+1:]
				break
			}
		}
		cmd.Env = env
	}
	return cmd
}

type transcriptSegment struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

func parseWhisperTranscript(data []byte, duration float64) (string, []transcriptSegment, error) {
	var raw struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Transcription []struct {
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if len(data) > 4<<20 || json.Unmarshal(data, &raw) != nil || len(raw.Transcription) > 1000 {
		return "", nil, fmt.Errorf("Whisper returned invalid or oversized transcription JSON")
	}
	segments := make([]transcriptSegment, 0, len(raw.Transcription))
	for _, item := range raw.Transcription {
		body := strings.TrimSpace(item.Text)
		if len(body) > 2000 || item.Offsets.From < 0 || item.Offsets.To < item.Offsets.From || float64(item.Offsets.To) > (duration+30)*1000 {
			return "", nil, fmt.Errorf("Whisper returned an invalid time range or segment")
		}
		if body != "" {
			segments = append(segments, transcriptSegment{StartMS: item.Offsets.From, EndMS: item.Offsets.To, Text: body})
		}
	}
	return strings.TrimSpace(raw.Result.Language), segments, nil
}

func (s *Service) runVideoTranscribe(ctx context.Context, jobID string) {
	defer func() { s.mu.Lock(); delete(s.cancels, jobID); s.mu.Unlock() }()
	select {
	case s.mediaSem <- struct{}{}:
		defer func() { <-s.mediaSem }()
	case <-ctx.Done():
		s.finishTranscriptContext(jobID, ctx.Err())
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
	whisper, err := discoverWhisperRuntime()
	if err != nil || whisper.modelDigest != job.ModelRevision {
		s.finishMediaFailure(jobID, "failed", "processor_unavailable", "the local Whisper runtime or model changed; start a new transcription job")
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "processor_unavailable", "ffmpeg is not installed")
		return
	}
	tempDir, err := os.MkdirTemp("", "hermetrix-transcribe-*")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "temp_unavailable", err.Error())
		return
	}
	defer os.RemoveAll(tempDir)
	wav := filepath.Join(tempDir, "audio.wav")
	output, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-threads", "2", "-protocol_whitelist", "file", "-i", file.Name(), "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-t", "600", "-y", wav).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			s.finishTranscriptContext(jobID, ctx.Err())
		} else {
			s.finishMediaFailure(jobID, "failed", "no_audio_or_decode_failed", fmt.Sprintf("video audio could not be decoded: %v %s", err, string(output[:min(len(output), 200)])))
		}
		return
	}
	info, err := os.Stat(wav)
	if err != nil || info.Size() < 44 || info.Size() > 32<<20 {
		s.finishMediaFailure(jobID, "failed", "resource_limit", "decoded audio exceeded the 32 MiB limit")
		return
	}
	durability.Exec("record video transcription progress").Observe(s.store.DB.ExecContext(context.Background(), `UPDATE media_jobs SET progress=.35,updated_at=? WHERE id=? AND state='running'`, formatTime(time.Now().UTC()), jobID))
	prefix := filepath.Join(tempDir, "transcript")
	output, err = whisper.command(ctx, "-m", whisper.modelPath, "-f", wav, "-l", "auto", "-t", "2", "-ng", "-oj", "-of", prefix, "-np").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			s.finishTranscriptContext(jobID, ctx.Err())
		} else {
			s.finishMediaFailure(jobID, "failed", "transcription_failed", fmt.Sprintf("Whisper failed: %v %s", err, string(output[:min(len(output), 200)])))
		}
		return
	}
	jsonFile, err := os.Open(prefix + ".json")
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "transcription_failed", "Whisper did not write a transcript")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(jsonFile, 4<<20+1))
	jsonFile.Close()
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "transcription_failed", err.Error())
		return
	}
	language, segments, err := parseWhisperTranscript(raw, duration)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "transcription_failed", err.Error())
		return
	}
	if ctx.Err() != nil {
		s.finishTranscriptContext(jobID, ctx.Err())
		return
	}
	body, _ := json.Marshal(map[string]any{"schema": "hermetrix.video-transcript.v1", "source_artifact_id": artifact.ID, "source_hash": artifact.Checksum,
		"model_sha256": whisper.modelDigest, "language": language, "duration_seconds": duration, "segments": segments,
		"limitations": []string{"automatic speech recognition may be inaccurate; audio without speech may produce no segments; visual content and unspoken sounds were not interpreted"}})
	ref, err := s.store.Blobs.Put(body)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "cas_write_failed", err.Error())
		return
	}
	created := time.Now().UTC()
	derivedID := identity.New("artifact")
	metadata, _ := json.Marshal(map[string]any{"media_job_id": job.ID, "source_artifact_id": artifact.ID, "source_hash": artifact.Checksum,
		"processor_revision": job.ProcessorRevision, "model_sha256": whisper.modelDigest, "language": language, "segment_count": len(segments)})
	tx, err := s.store.DB.BeginTx(context.Background(), nil)
	if err != nil {
		s.finishMediaFailure(jobID, "failed", "store_error", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id,source_lineage_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, derivedID, nullIfEmpty(artifact.ProjectID), filepath.Base(artifact.Name)+".transcript.json", "video_transcript",
		"application/vnd.hermetrix.video-transcript+json", ref, len(body), ref, string(metadata), formatTime(created), job.OwnerPrincipalID, fmt.Sprintf("[%q]", artifact.ID))
	if err == nil {
		_, err = tx.Exec(`INSERT INTO artifact_derivations(id,owner_principal_id,source_artifact_id,result_artifact_id,relation_kind,source_hash,result_hash,metadata_json,created_at)
			VALUES(?,?,?,?,?,?,?,?,?)`, identity.New("derivation"), job.OwnerPrincipalID, artifact.ID, derivedID, "audio_transcribe", artifact.Checksum, ref, string(metadata), formatTime(created))
	}
	if err == nil {
		ids, _ := json.Marshal([]string{derivedID})
		var result interface{ RowsAffected() (int64, error) }
		result, err = tx.Exec(`UPDATE media_jobs SET state='completed',progress=1,result_artifact_ids_json=?,updated_at=?,completed_at=? WHERE id=? AND state='running' AND cancel_requested=0`, string(ids), formatTime(created), formatTime(created), job.ID)
		if err == nil {
			count, _ := result.RowsAffected()
			if count != 1 {
				err = fmt.Errorf("transcription job was cancelled before commit")
			}
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if ctx.Err() != nil {
			s.finishTranscriptContext(jobID, ctx.Err())
		} else {
			s.finishMediaFailure(jobID, "failed", "commit_failed", err.Error())
		}
	}
}

func (s *Service) finishTranscriptContext(jobID string, cause error) {
	if errors.Is(cause, context.DeadlineExceeded) {
		s.finishMediaFailure(jobID, "failed", "resource_limit", "transcription exceeded the 1800 second wall deadline")
	} else {
		s.finishMediaFailure(jobID, "cancelled", "cancelled", "transcription was stopped")
	}
}
