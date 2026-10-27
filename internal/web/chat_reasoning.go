package web

import (
	"hermetrix-harness/internal/agent"
	"net/http"
)

func (s *Server) setSessionReasoning(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent unavailable"})
		return
	}
	var input agent.SessionReasoningInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.agent.SetSessionReasoning(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
