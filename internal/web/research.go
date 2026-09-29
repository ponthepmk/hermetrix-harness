package web

import (
	"errors"
	"net/http"

	"hermetrix-harness/internal/product"
)

func (s *Server) listTaskResearch(w http.ResponseWriter, r *http.Request) {
	if s.product == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "research is unavailable"})
		return
	}
	items, err := s.product.ListResearchArticles(r.Context(), r.PathValue("id"))
	if err != nil {
		researchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) searchTaskResearch(w http.ResponseWriter, r *http.Request) {
	if s.product == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "research is unavailable"})
		return
	}
	var input struct {
		Query string `json:"query"`
		Days  int    `json:"days"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	items, err := s.product.SearchResearchArticles(r.Context(), r.PathValue("id"), input.Query, input.Days)
	if err != nil {
		researchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) selectTaskResearch(w http.ResponseWriter, r *http.Request) {
	if s.product == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "research is unavailable"})
		return
	}
	var input struct {
		Selected bool `json:"selected"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.product.SelectResearchArticle(r.Context(), r.PathValue("id"), r.PathValue("candidateID"), input.Selected)
	if err != nil {
		researchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func researchError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, product.ErrInvalidResearchQuery) || errors.Is(err, product.ErrInvalidResearchRecency) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, product.ErrResearchNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, product.ErrResearchEgressBlocked) {
		status = http.StatusForbidden
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
