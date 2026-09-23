package web

import (
	"database/sql"
	"errors"
	"net/http"

	"hermetrix-harness/internal/product"
)

func (s *Server) registerDebuggerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/debug/capabilities", s.debugCapabilities)
	mux.HandleFunc("GET /api/debug/sessions", s.listDebugSessions)
	mux.HandleFunc("POST /api/debug/sessions", s.startDebugSession)
	mux.HandleFunc("GET /api/debug/sessions/{id}", s.getDebugSession)
	mux.HandleFunc("POST /api/debug/sessions/{id}/control", s.controlDebugSession)
	mux.HandleFunc("PUT /api/debug/sessions/{id}/breakpoints", s.setDebugBreakpoints)
	mux.HandleFunc("GET /api/debug/sessions/{id}/variables", s.debugVariables)
}
func debuggerError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status = http.StatusNotFound
	case errors.Is(err, product.ErrDebuggerConflict):
		status = http.StatusConflict
	case errors.Is(err, product.ErrDebuggerUnavailable):
		status = http.StatusNotImplemented
	}
	writeJSON(w, status, map[string]any{"error": err.Error()})
}
func (s *Server) debugCapabilities(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.product.DebugCapabilities())
}
func (s *Server) listDebugSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.ListDebugSessions(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (s *Server) startDebugSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.DebugStartInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.StartDebugSession(r.Context(), input)
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (s *Server) getDebugSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	item, err := s.product.GetDebugSession(r.Context(), r.PathValue("id"))
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) controlDebugSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.ControlDebugSession(r.Context(), r.PathValue("id"), input.Action)
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) setDebugBreakpoints(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Breakpoints []product.DebugBreakpoint `json:"breakpoints"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SetDebugBreakpoints(r.Context(), r.PathValue("id"), input.Breakpoints)
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (s *Server) debugVariables(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.DebugVariables(r.Context(), r.PathValue("id"), r.URL.Query().Get("frame_id"))
	if err != nil {
		debuggerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
