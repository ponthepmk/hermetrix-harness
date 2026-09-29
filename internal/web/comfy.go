package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"hermetrix-harness/internal/mcp"
)

type comfyReadiness struct {
	Connected     bool   `json:"connected"`
	Discovered    bool   `json:"discovered"`
	ServerRunning bool   `json:"server_running"`
	ToolCount     int    `json:"tool_count"`
	Reason        string `json:"reason,omitempty"`
}

func (s *Server) connectLocalComfy(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP is unavailable"})
		return
	}
	var input struct {
		Executable string `json:"executable"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	path := strings.TrimSpace(input.Executable)
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Comfy MCP executable path is required"})
		return
	}
	if err := mcp.ValidateLocalComfyExecutable(path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	servers, err := s.mcp.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	id := ""
	for _, server := range servers {
		if server.Name == "Comfy Local" && server.TransportKind == mcp.TransportStdio {
			id = server.ID
			break
		}
	}
	server, err := s.mcp.Save(r.Context(), mcp.SaveInput{ID: id, Name: "Comfy Local", TransportKind: mcp.TransportStdio, Executable: path,
		ProtocolMode: mcp.ProtocolAuto, RequestTimeoutMS: 30000, TrustAnnotations: false})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	discovery, err := s.mcp.Discover(r.Context(), server.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": server, "discovery": discovery})
}

func (s *Server) localComfyReadiness(w http.ResponseWriter, r *http.Request) {
	if s.mcp == nil || s.catalog == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP is unavailable"})
		return
	}
	servers, err := s.mcp.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	status := comfyReadiness{Reason: "Connect the installed Comfy MCP first"}
	var server *mcp.Server
	for i := range servers {
		if servers[i].Name == "Comfy Local" && servers[i].TransportKind == mcp.TransportStdio {
			server = &servers[i]
			break
		}
	}
	if server == nil {
		writeJSON(w, http.StatusOK, status)
		return
	}
	status.Connected, status.ToolCount = true, server.ToolCount
	if server.Status != "ready" {
		status.Reason = "Discover the Comfy MCP tools before checking its engine"
		writeJSON(w, http.StatusOK, status)
		return
	}
	status.Discovered = true
	for _, hit := range s.catalog.Search("server_info", "mcp", 50) {
		if hit.SourceRef != server.ID || hit.Name != "server_info" {
			continue
		}
		entry, err := s.catalog.Describe(hit.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		output, _, err := s.catalog.Call(r.Context(), entry.ID, entry.Revision, json.RawMessage(`{}`))
		if err != nil {
			status.Reason = "Comfy MCP is connected, but its engine status could not be read: " + err.Error()
			writeJSON(w, http.StatusOK, status)
			return
		}
		var envelope struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if json.Unmarshal([]byte(output.Output), &envelope) != nil || envelope.IsError {
			status.Reason = "Comfy MCP returned an invalid status"
			writeJSON(w, http.StatusOK, status)
			return
		}
		for _, part := range envelope.Content {
			if part.Type != "text" {
				continue
			}
			var info struct {
				Server struct {
					Running bool `json:"running"`
				} `json:"server"`
			}
			if json.Unmarshal([]byte(part.Text), &info) == nil {
				status.ServerRunning = info.Server.Running
				status.Reason = ""
				if !status.ServerRunning {
					status.Reason = "ComfyUI is installed but not running"
				}
				writeJSON(w, http.StatusOK, status)
				return
			}
		}
		status.Reason = "Comfy MCP did not return an engine status"
		writeJSON(w, http.StatusOK, status)
		return
	}
	status.Reason = "Comfy MCP did not publish server_info"
	writeJSON(w, http.StatusOK, status)
}
