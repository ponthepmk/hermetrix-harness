package web

import (
	"hermetrix-harness/internal/automation"
	"net/http"
)

func (s *Server) requireAutomation(w http.ResponseWriter) bool {
	if s.automations == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "automations unavailable"})
		return false
	}
	return true
}
func (s *Server) listAutomations(w http.ResponseWriter, r *http.Request) {
	if !s.requireAutomation(w) {
		return
	}
	items, err := s.automations.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) saveAutomation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAutomation(w) {
		return
	}
	var input automation.Input
	if !decodeJSONLimit(w, r, &input, 20000) {
		return
	}
	item, err := s.automations.Save(r.Context(), input)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if input.ID != "" {
		status = http.StatusOK
	}
	writeJSON(w, status, item)
}
func (s *Server) listAutomationRuns(w http.ResponseWriter, r *http.Request) {
	if !s.requireAutomation(w) {
		return
	}
	items, err := s.automations.Runs(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) triggerAutomation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAutomation(w) {
		return
	}
	var input struct {
		RequestID string `json:"request_id"`
	}
	if !decodeJSONLimit(w, r, &input, 1024) {
		return
	}
	item, err := s.automations.Trigger(r.Context(), r.PathValue("id"), input.RequestID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
