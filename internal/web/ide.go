package web

import (
	"hermetrix-harness/internal/product"
	"net/http"
)

func (s *Server) registerIDERoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/ide", s.projectIDECapabilities)
	mux.HandleFunc("POST /api/projects/{id}/ide/format", s.formatIDEBuffer)
	mux.HandleFunc("GET /api/projects/{id}/ide/jobs/{job}", s.getIDEJob)
}

func (s *Server) projectIDECapabilities(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	result, err := s.product.ProjectIDECapabilities(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) formatIDEBuffer(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.IDEFormatInput
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.product.FormatIDEBuffer(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getIDEJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	result, err := s.product.IDEJob(r.Context(), r.PathValue("id"), r.PathValue("job"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
