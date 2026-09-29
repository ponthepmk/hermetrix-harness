package web

import (
	"errors"
	"net/http"

	"hermetrix-harness/internal/product"
)

func (s *Server) getTaskContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	workspace, err := s.product.GetContentWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *Server) suggestTaskContent(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	items, err := s.product.SuggestContentDrafts(r.Context(), r.PathValue("id"))
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) saveTaskContentBrief(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ContentBrief
	if !decodeJSON(w, r, &input) {
		return
	}
	brief, err := s.product.SaveContentBrief(r.Context(), r.PathValue("id"), input)
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, brief)
}

func (s *Server) createTaskContentClaim(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ContentClaimInput
	if !decodeJSON(w, r, &input) {
		return
	}
	claim, err := s.product.CreateContentClaim(r.Context(), r.PathValue("id"), input)
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, claim)
}

func (s *Server) reviewTaskContentClaim(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		Verdict string `json:"verdict"`
		Actor   string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	claim, err := s.product.ReviewContentClaim(r.Context(), r.PathValue("id"), r.PathValue("claimID"), input.Verdict, effectiveActor(r.Context(), input.Actor))
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claim)
}

func (s *Server) removeTaskContentClaim(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	if err := s.product.RemoveContentClaim(r.Context(), r.PathValue("id"), r.PathValue("claimID")); err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

func (s *Server) saveTaskContentDraft(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input product.ContentDraftInput
	if !decodeJSON(w, r, &input) {
		return
	}
	draft, err := s.product.SaveContentDraft(r.Context(), r.PathValue("id"), input)
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) approveTaskContentDraft(w http.ResponseWriter, r *http.Request) {
	if !s.requireProduct(w) {
		return
	}
	var input struct {
		SubjectHash string `json:"subject_hash"`
		Actor       string `json:"actor"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	draft, err := s.product.ApproveContentDraft(r.Context(), r.PathValue("id"), r.PathValue("draftID"), input.SubjectHash, effectiveActor(r.Context(), input.Actor))
	if err != nil {
		contentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func contentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, product.ErrResearchNotFound):
		status = http.StatusNotFound
	case errors.Is(err, product.ErrContentInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, product.ErrContentStale):
		status = http.StatusConflict
	case errors.Is(err, product.ErrContentNotReady):
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
