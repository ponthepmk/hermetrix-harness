package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/product"
)

type comfyJob struct {
	ID           string   `json:"id"`
	TaskID       string   `json:"task_id"`
	ProjectID    string   `json:"project_id"`
	WorkflowPath string   `json:"workflow_path"`
	WorkflowHash string   `json:"workflow_hash"`
	State        string   `json:"state"`
	PromptID     string   `json:"prompt_id,omitempty"`
	ArtifactIDs  []string `json:"artifact_ids"`
	Error        string   `json:"error,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	stagedPath   string
	operationID  string
}

var comfyOperationPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{15,127}$`)

func (s *Server) readComfyJob(r *http.Request, id string) (comfyJob, error) {
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		return comfyJob{}, err
	}
	var job comfyJob
	var artifacts string
	err = s.store.DB.QueryRowContext(r.Context(), `SELECT id,task_id,project_id,workflow_path,workflow_hash,staged_path,
		operation_id,state,prompt_id,result_artifact_ids_json,error,created_at,updated_at FROM task_comfy_jobs
		WHERE id=? AND owner_principal_id=?`, id, owner).Scan(&job.ID, &job.TaskID, &job.ProjectID, &job.WorkflowPath,
		&job.WorkflowHash, &job.stagedPath, &job.operationID, &job.State, &job.PromptID, &artifacts, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return comfyJob{}, err
	}
	if json.Unmarshal([]byte(artifacts), &job.ArtifactIDs) != nil {
		return comfyJob{}, fmt.Errorf("stored Comfy artifact list is invalid")
	}
	return job, nil
}

func (s *Server) listComfyJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) || !s.requireTaskEngine(w) {
		return
	}
	taskID := r.URL.Query().Get("task_id")
	if _, err := s.tasks.Get(r.Context(), taskID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Task not found"})
		return
	}
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := s.store.DB.QueryContext(r.Context(), `SELECT id FROM task_comfy_jobs WHERE task_id=? AND owner_principal_id=? ORDER BY created_at DESC LIMIT 50`, taskID, owner)
	if err != nil {
		writeError(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	items := []comfyJob{}
	for _, id := range ids {
		job, err := s.readComfyJob(r, id)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, job)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) startComfyJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) || !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		TaskID       string `json:"task_id"`
		WorkflowPath string `json:"workflow_path"`
		WorkflowHash string `json:"workflow_hash"`
		OperationID  string `json:"operation_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !comfyOperationPattern.MatchString(input.OperationID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation_id must be a stable 16–128 character identifier"})
		return
	}
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	var existing string
	err = s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM task_comfy_jobs WHERE owner_principal_id=? AND operation_id=?`, owner, input.OperationID).Scan(&existing)
	if err == nil {
		job, readErr := s.readComfyJob(r, existing)
		if readErr != nil {
			writeError(w, readErr)
			return
		}
		if job.TaskID != input.TaskID || job.WorkflowHash != input.WorkflowHash {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "operation_id belongs to a different workflow"})
			return
		}
		writeJSON(w, http.StatusOK, job)
		return
	}
	if err != sql.ErrNoRows {
		writeError(w, err)
		return
	}
	check, status, err := s.checkComfyWorkflow(r, input.TaskID, input.WorkflowPath)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if !check.Ready {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "workflow is not ready for a local no-credit run", "preflight": check})
		return
	}
	if check.WorkflowHash != input.WorkflowHash {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "workflow changed since approval; check it again"})
		return
	}
	task, err := s.tasks.Get(r.Context(), input.TaskID)
	if err != nil {
		writeError(w, err)
		return
	}
	_, body, err := s.comfyWorkflowFile(r, input.TaskID, check.WorkflowPath)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != check.WorkflowHash {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "workflow changed during submission"})
		return
	}
	jobID := identity.New("comfyjob")
	stagedDir := filepath.Join(s.store.Root, "comfy", "workflows")
	if err := os.MkdirAll(stagedDir, 0700); err != nil {
		writeError(w, err)
		return
	}
	stagedPath := filepath.Join(stagedDir, jobID+".json")
	file, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		writeError(w, err)
		return
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		writeError(w, fmt.Errorf("stage workflow: %v %v", writeErr, closeErr))
		return
	}
	server, err := s.readyComfyServer(r)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err := s.requireLocalComfyTarget(r, server.ID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	// Validate the immutable copy as well; the user supplied path can change
	// while the first preflight is running.
	validation, err := s.callComfyTool(r, server.ID, "validate_workflow", map[string]any{"workflow_path": stagedPath})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	var staged struct {
		Valid            bool              `json:"valid"`
		SpendsCredits    bool              `json:"spends_credits"`
		PartnerNodes     []json.RawMessage `json:"partner_nodes"`
		ObjectInfoSource struct {
			Mode string `json:"mode"`
			Host string `json:"host"`
		} `json:"object_info_source"`
	}
	if json.Unmarshal(validation, &staged) != nil || !staged.Valid || staged.SpendsCredits || len(staged.PartnerNodes) > 0 || staged.ObjectInfoSource.Mode != "local" || !comfyLoopbackHost(staged.ObjectInfoSource.Host) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "staged workflow did not pass local no-credit validation"})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.store.DB.ExecContext(r.Context(), `INSERT INTO task_comfy_jobs(id,task_id,project_id,owner_principal_id,workflow_path,workflow_hash,staged_path,operation_id,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, jobID, input.TaskID, task.ProjectID, owner, check.WorkflowPath, check.WorkflowHash, stagedPath, input.OperationID, "submitting", now, now)
	if err != nil {
		var concurrent string
		if s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM task_comfy_jobs WHERE owner_principal_id=? AND operation_id=?`, owner, input.OperationID).Scan(&concurrent) == nil {
			job, readErr := s.readComfyJob(r, concurrent)
			if readErr == nil && job.TaskID == input.TaskID && job.WorkflowHash == input.WorkflowHash {
				writeJSON(w, http.StatusOK, job)
				return
			}
		}
		writeError(w, err)
		return
	}
	// The durable submission row exists before the effectful MCP call. A lost
	// response is uncertain and must never cause a second generation.
	raw, callErr := s.callComfyTool(r, server.ID, "run_workflow", map[string]any{"workflow_path": stagedPath, "wait": false, "confirm_spend": false})
	state, promptID, issue := "queued", "", ""
	if callErr != nil {
		state = "uncertain"
		issue = callErr.Error()
	} else {
		var submitted struct {
			PromptID string `json:"prompt_id"`
		}
		if json.Unmarshal(raw, &submitted) != nil || submitted.PromptID == "" {
			state = "uncertain"
			issue = "Comfy submission returned no prompt ID"
		} else {
			promptID = submitted.PromptID
		}
	}
	_, err = s.store.DB.ExecContext(r.Context(), `UPDATE task_comfy_jobs SET state=?,prompt_id=?,error=?,updated_at=? WHERE id=? AND owner_principal_id=?`, state, promptID, issue, time.Now().UTC().Format(time.RFC3339Nano), jobID, owner)
	if err != nil {
		writeError(w, err)
		return
	}
	job, err := s.readComfyJob(r, jobID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) refreshComfyJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	s.comfyMu.Lock()
	defer s.comfyMu.Unlock()
	job, err := s.readComfyJob(r, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Comfy job not found"})
		return
	}
	if job.State == "submitting" {
		if err := s.setComfyJobState(r, job.ID, "uncertain", job.PromptID, "submission was interrupted; inspect Comfy queue before any new run", nil); err != nil {
			writeError(w, err)
			return
		}
		job, err = s.readComfyJob(r, job.ID)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if job.PromptID == "" || job.State == "completed" || job.State == "failed" || job.State == "cancelled" {
		writeJSON(w, http.StatusOK, job)
		return
	}
	server, err := s.readyComfyServer(r)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err := s.requireLocalComfyTarget(r, server.ID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	raw, err := s.callComfyTool(r, server.ID, "job", map[string]any{"action": "status", "prompt_id": job.PromptID})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	var remote struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &remote) != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Comfy returned an invalid job status"})
		return
	}
	switch remote.Status {
	case "completed":
		ids, ingestErr := s.ingestComfyOutputs(r, server.ID, job)
		if ingestErr != nil {
			err = s.setComfyJobState(r, job.ID, "uncertain", job.PromptID, "output ingestion needs review: "+ingestErr.Error(), ids)
		} else {
			err = s.setComfyJobState(r, job.ID, "completed", job.PromptID, "", ids)
		}
	case "failed", "cancelled":
		err = s.setComfyJobState(r, job.ID, remote.Status, job.PromptID, string(remote.Error), nil)
	case "queued", "running":
		err = s.setComfyJobState(r, job.ID, remote.Status, job.PromptID, "", nil)
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Comfy returned an unknown job status"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	job, err = s.readComfyJob(r, job.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) cancelComfyJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	s.comfyMu.Lock()
	defer s.comfyMu.Unlock()
	job, err := s.readComfyJob(r, r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Comfy job not found"})
		return
	}
	if job.PromptID == "" || (job.State != "queued" && job.State != "running") {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "only a known queued or running job can be cancelled"})
		return
	}
	server, err := s.readyComfyServer(r)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if err := s.requireLocalComfyTarget(r, server.ID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	_, err = s.callComfyTool(r, server.ID, "job", map[string]any{"action": "cancel", "prompt_id": job.PromptID})
	if err != nil {
		err = s.setComfyJobState(r, job.ID, "uncertain", job.PromptID, "cancellation outcome is unknown: "+err.Error(), nil)
	} else {
		err = s.setComfyJobState(r, job.ID, "uncertain", job.PromptID, "cancellation requested; check Comfy status", nil)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	job, err = s.readComfyJob(r, job.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) setComfyJobState(r *http.Request, id, state, promptID, issue string, artifacts []string) error {
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		return err
	}
	if artifacts == nil {
		_, err = s.store.DB.ExecContext(r.Context(), `UPDATE task_comfy_jobs SET state=?,prompt_id=?,error=?,updated_at=? WHERE id=? AND owner_principal_id=?`, state, promptID, issue, time.Now().UTC().Format(time.RFC3339Nano), id, owner)
	} else {
		encoded, _ := json.Marshal(artifacts)
		_, err = s.store.DB.ExecContext(r.Context(), `UPDATE task_comfy_jobs SET state=?,prompt_id=?,result_artifact_ids_json=?,error=?,updated_at=? WHERE id=? AND owner_principal_id=?`, state, promptID, string(encoded), issue, time.Now().UTC().Format(time.RFC3339Nano), id, owner)
	}
	return err
}

func (s *Server) ingestComfyOutputs(r *http.Request, serverID string, job comfyJob) ([]string, error) {
	outDir := filepath.Join(s.store.Root, "comfy", "outputs", job.ID)
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return nil, err
	}
	raw, err := s.callComfyTool(r, serverID, "fetch_outputs", map[string]any{"prompt_id": job.PromptID, "out_dir": outDir, "inline_images": false})
	if err != nil {
		return nil, err
	}
	var result struct {
		Files []struct {
			Path string `json:"path"`
			Size int64  `json:"size"`
		} `json:"files"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Files) == 0 || len(result.Files) > 20 {
		return nil, fmt.Errorf("Comfy returned no bounded output file list")
	}
	ids := []string{}
	owner, err := s.store.OwnerPrincipalID(r.Context())
	if err != nil {
		return nil, err
	}
	for _, entry := range result.Files {
		clean, err := filepath.EvalSymlinks(entry.Path)
		if err != nil {
			return ids, err
		}
		rel, err := filepath.Rel(outDir, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return ids, fmt.Errorf("Comfy output escaped the private job folder")
		}
		file, err := os.Open(clean)
		if err != nil {
			return ids, err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 256<<20 || info.Size() != entry.Size {
			file.Close()
			return ids, fmt.Errorf("Comfy output failed size validation")
		}
		var prior string
		if s.store.DB.QueryRowContext(r.Context(), `SELECT id FROM artifacts WHERE owner_principal_id=? AND json_extract(metadata_json,'$.comfy_job_id')=? AND json_extract(metadata_json,'$.comfy_output_name')=? LIMIT 1`, owner, job.ID, filepath.Base(clean)).Scan(&prior) == nil {
			file.Close()
			ids = append(ids, prior)
			continue
		}
		var artifact product.Artifact
		switch strings.ToLower(filepath.Ext(clean)) {
		case ".mp4", ".webm":
			artifact, err = s.product.UploadVideo(r.Context(), product.VideoUploadInput{ProjectID: job.ProjectID, Name: filepath.Base(clean), Reader: file})
		case ".png", ".jpg", ".jpeg", ".webp":
			if info.Size() > 8<<20 {
				err = fmt.Errorf("Comfy image exceeds 8 MiB")
			} else {
				var data []byte
				data, err = io.ReadAll(io.LimitReader(file, 8<<20+1))
				if err == nil {
					artifact, err = s.product.UploadMedia(r.Context(), product.MediaUploadInput{ProjectID: job.ProjectID, Name: filepath.Base(clean), Data: data})
					// Comfy's built-in animation node exports WebP. Convert only a
					// detected animation to a bounded MP4, then run the same ffprobe
					// validation as any uploaded video before it can enter a draft.
					if err != nil && strings.EqualFold(filepath.Ext(clean), ".webp") && isAnimatedWebP(data) {
						artifact, err = s.importComfyAnimatedWebP(r, job, clean)
					}
				}
			}
		default:
			err = fmt.Errorf("unsupported Comfy output type: %s", filepath.Ext(clean))
		}
		file.Close()
		if err != nil {
			return ids, err
		}
		if artifact.Metadata == nil {
			artifact.Metadata = map[string]any{}
		}
		artifact.Metadata["source"] = "comfy-mcp"
		artifact.Metadata["comfy_job_id"] = job.ID
		artifact.Metadata["comfy_prompt_id"] = job.PromptID
		artifact.Metadata["comfy_workflow_hash"] = job.WorkflowHash
		artifact.Metadata["comfy_output_name"] = filepath.Base(clean)
		metadata, _ := json.Marshal(artifact.Metadata)
		if _, err := s.store.DB.ExecContext(r.Context(), `UPDATE artifacts SET metadata_json=? WHERE id=? AND owner_principal_id=?`, string(metadata), artifact.ID, owner); err != nil {
			return ids, err
		}
		ids = append(ids, artifact.ID)
	}
	return ids, nil
}

func isAnimatedWebP(data []byte) bool {
	return len(data) >= 30 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" && string(data[12:16]) == "VP8X" && data[20]&0x02 != 0
}

func (s *Server) importComfyAnimatedWebP(r *http.Request, job comfyJob, path string) (product.Artifact, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return product.Artifact{}, fmt.Errorf("animated WebP needs installed ffmpeg to become a video")
	}
	output := strings.TrimSuffix(path, filepath.Ext(path)) + ".mp4"
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	arguments := []string{"-nostdin", "-v", "error", "-protocol_whitelist", "file", "-i", path, "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-t", "600", "-fs", "268435456", "-y", output}
	message, err := exec.CommandContext(ctx, ffmpeg, arguments...).CombinedOutput()
	if err != nil {
		if len(message) > 240 {
			message = message[:240]
		}
		return product.Artifact{}, fmt.Errorf("convert Comfy animation: %v %s", err, message)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() < 1 || info.Size() > 256<<20 {
		return product.Artifact{}, fmt.Errorf("converted Comfy video exceeds validated size limit")
	}
	file, err := os.Open(output)
	if err != nil {
		return product.Artifact{}, err
	}
	defer file.Close()
	return s.product.UploadVideo(r.Context(), product.VideoUploadInput{ProjectID: job.ProjectID, Name: filepath.Base(output), Reader: file})
}
