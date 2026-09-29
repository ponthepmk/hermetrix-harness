package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/taskengine"
)

func TestComfyReadinessStartsDisconnectedAndRejectsGenericLauncher(t *testing.T) {
	server := testHTTPServer(t)
	var readiness comfyReadiness
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/readiness", http.MethodGet, nil, http.StatusOK), &readiness); err != nil {
		t.Fatal(err)
	}
	if readiness.Connected {
		t.Fatal("Comfy unexpectedly connected in a fresh store")
	}
	requestJSON(t, server.URL+"/api/comfy/connect", http.MethodPost, map[string]string{"executable": "npx"}, http.StatusBadRequest)
}

func TestInstalledComfyReadinessThroughHermetrixAPI(t *testing.T) {
	path := os.Getenv("HERMETRIX_COMFY_MCP_PATH")
	if path == "" {
		t.Skip("set HERMETRIX_COMFY_MCP_PATH for local integration check")
	}
	server := testHTTPServer(t)
	requestJSON(t, server.URL+"/api/comfy/connect", http.MethodPost, map[string]string{"executable": path}, http.StatusOK)
	var readiness comfyReadiness
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/readiness", http.MethodGet, nil, http.StatusOK), &readiness); err != nil {
		t.Fatal(err)
	}
	if !readiness.Connected || !readiness.Discovered || readiness.ToolCount < 5 {
		t.Fatalf("Comfy readiness=%+v", readiness)
	}
	if readiness.Reason != "" && !strings.Contains(readiness.Reason, "not running") {
		t.Fatalf("could not inspect Comfy engine: %+v", readiness)
	}
}

func TestComfyPreflightRejectsWorkflowOutsideTaskProject(t *testing.T) {
	server := testHTTPServer(t)
	var project struct {
		ID string `json:"id"`
	}
	root := t.TempDir()
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Comfy fixture", "root_path": root}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	var task taskengine.Task
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodPost, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Comfy Task", Objective: "Inspect a workflow", OriginalRequest: "Inspect a workflow", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Inspect workflow"}},
	}, http.StatusCreated), &task); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(outside, []byte(`{"1":{"class_type":"EmptyImage"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.URL+"/api/comfy/preflight", http.MethodPost,
		map[string]string{"task_id": task.ID, "workflow_path": outside}, http.StatusBadRequest)
	inside := filepath.Join(root, "workflow.json")
	if err := os.WriteFile(inside, []byte(`{"1":{"class_type":"EmptyImage"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.URL+"/api/comfy/preflight", http.MethodPost,
		map[string]string{"task_id": task.ID, "workflow_path": inside}, http.StatusConflict)
}

func TestInstalledComfyPreflightThroughHermetrixAPI(t *testing.T) {
	path := os.Getenv("HERMETRIX_COMFY_MCP_PATH")
	if path == "" {
		t.Skip("set HERMETRIX_COMFY_MCP_PATH for local integration check")
	}
	server := testHTTPServer(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost,
		map[string]any{"name": "Comfy integration fixture", "root_path": root}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	var task taskengine.Task
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodPost, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Comfy Integration", Objective: "Inspect local workflow", OriginalRequest: "Inspect local workflow", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Inspect workflow"}},
	}, http.StatusCreated), &task); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.URL+"/api/comfy/connect", http.MethodPost, map[string]string{"executable": path}, http.StatusOK)
	workflow := filepath.Join(root, "internal", "web", "testdata", "comfy-color-motion.json")
	var result comfyPreflight
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/preflight", http.MethodPost,
		map[string]string{"task_id": task.ID, "workflow_path": workflow}, http.StatusOK), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Ready || !result.Valid || !result.LocalOnly || result.SpendsCredits || result.NodeCount != 6 || len(result.WorkflowHash) != 64 {
		t.Fatalf("unexpected Comfy preflight: %+v", result)
	}
	var missing comfyPreflight
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/preflight", http.MethodPost,
		map[string]string{"task_id": task.ID, "workflow_path": filepath.Join(root, "internal", "web", "testdata", "comfy-missing-model.json")}, http.StatusOK), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Ready || missing.Valid || !strings.Contains(missing.Reason, "Hermetrix_missing_model_for_preflight.safetensors") {
		t.Fatalf("missing model prerequisite was hidden: %+v", missing)
	}
	requestJSON(t, server.URL+"/api/comfy/jobs", http.MethodPost, map[string]string{
		"task_id": task.ID, "workflow_path": workflow, "workflow_hash": strings.Repeat("0", 64), "operation_id": "stale-comfy-test-123456"}, http.StatusConflict)
}

func TestInstalledComfyJobPersistsAndIngestsOutput(t *testing.T) {
	path := os.Getenv("HERMETRIX_COMFY_MCP_PATH")
	if path == "" {
		t.Skip("set HERMETRIX_COMFY_MCP_PATH for local generation check")
	}
	server := testHTTPServer(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/projects", http.MethodPost, map[string]any{"name": "Comfy job fixture", "root_path": root}, http.StatusCreated), &project); err != nil {
		t.Fatal(err)
	}
	var task taskengine.Task
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/tasks", http.MethodPost, taskengine.CreateTaskInput{
		ProjectID: project.ID, Title: "Comfy job", Objective: "Make a local fixture", OriginalRequest: "Make a local fixture", Actor: "owner",
		Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "Capture output"}},
	}, http.StatusCreated), &task); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.URL+"/api/comfy/connect", http.MethodPost, map[string]string{"executable": path}, http.StatusOK)
	workflow := filepath.Join(root, "internal", "web", "testdata", "comfy-color-motion.json")
	var check comfyPreflight
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/preflight", http.MethodPost, map[string]string{"task_id": task.ID, "workflow_path": workflow}, http.StatusOK), &check); err != nil {
		t.Fatal(err)
	}
	if !check.Ready {
		t.Fatalf("preflight not ready: %+v", check)
	}
	input := map[string]string{"task_id": task.ID, "workflow_path": workflow, "workflow_hash": check.WorkflowHash, "operation_id": "comfy-job-test-123456"}
	var job comfyJob
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/jobs", http.MethodPost, input, http.StatusAccepted), &job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" || job.PromptID == "" || job.State != "queued" {
		t.Fatalf("unexpected submitted job: %+v", job)
	}
	var duplicate comfyJob
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/jobs", http.MethodPost, input, http.StatusOK), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != job.ID || duplicate.PromptID != job.PromptID {
		t.Fatalf("duplicate submission was not idempotent: %+v %+v", job, duplicate)
	}
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/jobs/"+job.ID+"/refresh", http.MethodPost, map[string]any{}, http.StatusOK), &job); err != nil {
			t.Fatal(err)
		}
		if job.State == "completed" {
			break
		}
		if job.State == "failed" || job.State == "uncertain" {
			t.Fatalf("Comfy job did not finish cleanly: %+v", job)
		}
		time.Sleep(250 * time.Millisecond)
	}
	if job.State != "completed" || len(job.ArtifactIDs) == 0 {
		t.Fatalf("Comfy output not ingested: %+v", job)
	}
	var artifacts []struct {
		ID       string         `json:"id"`
		Kind     string         `json:"kind"`
		MIMEType string         `json:"mime_type"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/artifacts", http.MethodGet, nil, http.StatusOK), &artifacts); err != nil {
		t.Fatal(err)
	}
	videoFound := false
	for _, artifact := range artifacts {
		duration, _ := artifact.Metadata["duration_seconds"].(float64)
		if artifact.ID == job.ArtifactIDs[0] && artifact.Kind == "video" && artifact.MIMEType == "video/mp4" && artifact.Metadata["comfy_job_id"] == job.ID && duration > 1 {
			videoFound = true
		}
	}
	if !videoFound {
		t.Fatalf("Comfy animation did not become a validated playable MP4: %+v", artifacts)
	}
	var listed []comfyJob
	if err := json.Unmarshal(requestJSON(t, server.URL+"/api/comfy/jobs?task_id="+task.ID, http.MethodGet, nil, http.StatusOK), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != job.ID {
		t.Fatalf("job list=%+v", listed)
	}
}
