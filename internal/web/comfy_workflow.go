package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"hermetrix-harness/internal/capabilities"
	"hermetrix-harness/internal/mcp"
)

type comfyPreflight struct {
	TaskID        string          `json:"task_id"`
	WorkflowPath  string          `json:"workflow_path"`
	WorkflowHash  string          `json:"workflow_hash"`
	NodeCount     int             `json:"node_count"`
	Valid         bool            `json:"valid"`
	LocalOnly     bool            `json:"local_only"`
	SpendsCredits bool            `json:"spends_credits"`
	Errors        json.RawMessage `json:"errors"`
	Warnings      json.RawMessage `json:"warnings"`
	Dependencies  json.RawMessage `json:"dependencies"`
	Ready         bool            `json:"ready"`
	Reason        string          `json:"reason,omitempty"`
}

func (s *Server) callComfyTool(r *http.Request, serverID, name string, args map[string]any) (json.RawMessage, error) {
	if s.catalog == nil {
		return nil, fmt.Errorf("Comfy capability catalog is unavailable")
	}
	for _, hit := range s.catalog.Search(name, "mcp", 50) {
		if hit.SourceRef != serverID || hit.Name != name {
			continue
		}
		entry, err := s.catalog.Describe(hit.ID)
		if err != nil {
			return nil, err
		}
		input, _ := json.Marshal(args)
		output, _, err := s.catalog.Call(r.Context(), entry.ID, entry.Revision, input)
		if err != nil {
			return nil, err
		}
		var envelope struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal([]byte(output.Output), &envelope); err != nil {
			return nil, fmt.Errorf("Comfy MCP returned an invalid envelope")
		}
		if envelope.IsError {
			return nil, fmt.Errorf("Comfy MCP reported an error")
		}
		for _, part := range envelope.Content {
			if part.Type == "text" && json.Valid([]byte(part.Text)) {
				return json.RawMessage(part.Text), nil
			}
		}
		return nil, fmt.Errorf("Comfy MCP returned no JSON result")
	}
	return nil, capabilities.ErrNotFound
}

func (s *Server) readyComfyServer(r *http.Request) (mcp.Server, error) {
	if s.mcp == nil {
		return mcp.Server{}, fmt.Errorf("Comfy MCP is unavailable")
	}
	servers, err := s.mcp.List(r.Context())
	if err != nil {
		return mcp.Server{}, err
	}
	for _, server := range servers {
		if server.Name == "Comfy Local" && server.TransportKind == mcp.TransportStdio && server.Status == "ready" {
			return server, nil
		}
	}
	return mcp.Server{}, fmt.Errorf("connect and discover Comfy MCP first")
}

func (s *Server) requireLocalComfyTarget(r *http.Request, serverID string) error {
	raw, err := s.callComfyTool(r, serverID, "server_info", map[string]any{})
	if err != nil {
		return err
	}
	var info struct {
		Server struct {
			Running bool   `json:"running"`
			URL     string `json:"url"`
		} `json:"server"`
		ComfyTarget json.RawMessage `json:"comfy_target"`
	}
	if json.Unmarshal(raw, &info) != nil || !info.Server.Running {
		return fmt.Errorf("local ComfyUI is not running")
	}
	if len(info.ComfyTarget) > 0 && string(info.ComfyTarget) != "null" {
		return fmt.Errorf("Comfy is configured with another target; local-only workflow run is blocked")
	}
	parsed, err := url.Parse(info.Server.URL)
	if err != nil || parsed.Scheme != "http" || !comfyLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("ComfyUI URL must be local loopback HTTP")
	}
	return nil
}

func comfyLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) preflightLocalComfyWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) || !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		TaskID       string `json:"task_id"`
		WorkflowPath string `json:"workflow_path"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, status, err := s.checkComfyWorkflow(r, input.TaskID, input.WorkflowPath)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) checkComfyWorkflow(r *http.Request, taskID, workflowPath string) (comfyPreflight, int, error) {
	path, body, err := s.comfyWorkflowFile(r, taskID, workflowPath)
	if err != nil {
		return comfyPreflight{}, http.StatusBadRequest, err
	}
	server, err := s.readyComfyServer(r)
	if err != nil {
		return comfyPreflight{}, http.StatusConflict, err
	}
	if err := s.requireLocalComfyTarget(r, server.ID); err != nil {
		return comfyPreflight{}, http.StatusConflict, err
	}
	var graph map[string]json.RawMessage
	if json.Unmarshal(body, &graph) != nil || len(graph) == 0 || graph["nodes"] != nil {
		return comfyPreflight{}, http.StatusBadRequest, fmt.Errorf("use a nonempty API-format Comfy workflow JSON")
	}
	nodeCount := 0
	for _, raw := range graph {
		var node struct {
			ClassType string `json:"class_type"`
		}
		if json.Unmarshal(raw, &node) != nil || node.ClassType == "" {
			return comfyPreflight{}, http.StatusBadRequest, fmt.Errorf("workflow contains a node without class_type")
		}
		nodeCount++
	}
	validation, err := s.callComfyTool(r, server.ID, "validate_workflow", map[string]any{"workflow_path": path})
	if err != nil {
		return comfyPreflight{}, http.StatusBadGateway, err
	}
	deps, depErr := s.callComfyTool(r, server.ID, "workflow_deps", map[string]any{"workflow_path": path})
	if depErr != nil {
		deps = json.RawMessage(`{}`)
	}
	var dependencies struct {
		Unsupported  bool     `json:"unsupported"`
		UnknownNodes []string `json:"unknown_nodes"`
		CustomNodes  map[string]struct {
			State string `json:"state"`
		} `json:"custom_nodes"`
	}
	if depErr == nil && json.Unmarshal(deps, &dependencies) != nil {
		depErr = fmt.Errorf("Comfy returned an invalid dependency result")
	}
	var verdict struct {
		Valid            bool              `json:"valid"`
		SpendsCredits    bool              `json:"spends_credits"`
		PartnerNodes     []json.RawMessage `json:"partner_nodes"`
		Errors           json.RawMessage   `json:"errors"`
		Warnings         json.RawMessage   `json:"warnings"`
		ObjectInfoSource struct {
			Mode string `json:"mode"`
			Host string `json:"host"`
		} `json:"object_info_source"`
	}
	if json.Unmarshal(validation, &verdict) != nil {
		return comfyPreflight{}, http.StatusBadGateway, fmt.Errorf("Comfy returned an invalid validation result")
	}
	sum := sha256.Sum256(body)
	result := comfyPreflight{TaskID: taskID, WorkflowPath: path, WorkflowHash: hex.EncodeToString(sum[:]), NodeCount: nodeCount,
		Valid: verdict.Valid, LocalOnly: verdict.ObjectInfoSource.Mode == "local" && comfyLoopbackHost(verdict.ObjectInfoSource.Host), SpendsCredits: verdict.SpendsCredits,
		Errors: verdict.Errors, Warnings: verdict.Warnings, Dependencies: deps}
	result.Ready = result.Valid && result.LocalOnly && !result.SpendsCredits && len(verdict.PartnerNodes) == 0
	if !result.Valid {
		result.Reason = "Comfy reports workflow errors"
		var findings []struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(verdict.Errors, &findings) == nil && len(findings) > 0 && findings[0].Message != "" {
			message := findings[0].Message
			if len(message) > 300 {
				message = message[:300]
			}
			result.Reason = "Comfy: " + message
		}
	}
	if !result.LocalOnly {
		result.Reason = "workflow validation did not use local ComfyUI"
	}
	if result.SpendsCredits || len(verdict.PartnerNodes) > 0 {
		result.Reason = "workflow may spend partner credits"
	}
	if depErr != nil {
		result.Ready = false
		result.Reason = "workflow dependencies could not be checked: " + depErr.Error()
	}
	if dependencies.Unsupported {
		result.Ready = false
		result.Reason = "Comfy Manager cannot check workflow dependencies"
	}
	if len(dependencies.UnknownNodes) > 0 {
		result.Ready = false
		result.Reason = "workflow contains unknown nodes"
	}
	for _, node := range dependencies.CustomNodes {
		if node.State != "installed" {
			result.Ready = false
			result.Reason = "workflow needs a missing node pack"
			break
		}
	}
	// A changed file invalidates the digest shown to the caller. A queued run
	// copies these bytes to an immutable private path and checks them again.
	_, after, err := s.comfyWorkflowFile(r, taskID, path)
	if err != nil {
		return comfyPreflight{}, http.StatusConflict, err
	}
	if sha256.Sum256(after) != sum {
		return comfyPreflight{}, http.StatusConflict, fmt.Errorf("workflow changed while it was being checked; retry preflight")
	}
	return result, http.StatusOK, nil
}

func (s *Server) comfyWorkflowFile(r *http.Request, taskID, rawPath string) (string, []byte, error) {
	if s.tasks == nil || s.product == nil {
		return "", nil, fmt.Errorf("Task or Project service is unavailable")
	}
	task, err := s.tasks.Get(r.Context(), taskID)
	if err != nil {
		return "", nil, err
	}
	if task.ProjectID == "" {
		return "", nil, fmt.Errorf("choose a Project with a folder for Comfy workflow files")
	}
	project, err := s.product.GetProject(r.Context(), task.ProjectID)
	if err != nil {
		return "", nil, err
	}
	if project.RootPath == "" {
		return "", nil, fmt.Errorf("this Project has no folder for workflow files")
	}
	if !filepath.IsAbs(rawPath) || !strings.EqualFold(filepath.Ext(rawPath), ".json") {
		return "", nil, fmt.Errorf("choose an absolute .json workflow file inside this Project")
	}
	root, err := filepath.EvalSymlinks(project.RootPath)
	if err != nil {
		return "", nil, err
	}
	path, err := filepath.EvalSymlinks(rawPath)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", nil, fmt.Errorf("workflow must stay inside this Project folder")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 2<<20 {
		return "", nil, fmt.Errorf("workflow must be a regular JSON file up to 2 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, 2<<20+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) > 2<<20 {
		return "", nil, fmt.Errorf("workflow exceeds 2 MiB")
	}
	return path, data, nil
}
