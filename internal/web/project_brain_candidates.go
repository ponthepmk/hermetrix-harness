package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/projectbrain"
)

type projectBrainSelection struct {
	TaskID              string   `json:"task_id"`
	ValidationID        string   `json:"validation_id"`
	SelectedArtifactIDs []string `json:"selected_artifact_ids"`
	Solution            string   `json:"solution"`
	VerificationKind    string   `json:"verification_kind"`
}

type projectBrainStageRequest struct {
	projectBrainSelection
	Approval projectbrain.CandidateExportApproval `json:"approval"`
}

func (s *Server) projectBrainConfigured(w http.ResponseWriter) bool {
	if s.brainStage == nil || s.brainOutbox == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			map[string]string{"error": "Project Brain candidate sharing is not configured"})
		return false
	}
	return true
}

func projectBrainSelectionError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	message := "Selected verified evidence is unavailable or no longer approved for sharing"
	if errors.Is(err, projectbrain.ErrCandidateConflict) {
		status = http.StatusConflict
		message = "This verified source already has a different Project Brain candidate"
	} else if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
		message = "Verified source was not found"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) getProjectBrainEligibility(w http.ResponseWriter, r *http.Request) {
	if !s.projectBrainConfigured(w) {
		return
	}
	result, err := s.brainStage.Eligibility(r.Context(), r.PathValue("id"))
	if err != nil {
		projectBrainSelectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) previewProjectBrainCandidate(w http.ResponseWriter, r *http.Request) {
	if !s.projectBrainConfigured(w) {
		return
	}
	var input projectBrainSelection
	if !decodeJSONLimit(w, r, &input, 32<<10) {
		return
	}
	result, err := s.brainStage.Preview(r.Context(), input.TaskID, input.ValidationID,
		input.SelectedArtifactIDs, input.Solution, input.VerificationKind)
	if err != nil {
		projectBrainSelectionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) stageProjectBrainCandidate(w http.ResponseWriter, r *http.Request) {
	if !s.projectBrainConfigured(w) {
		return
	}
	var input projectBrainStageRequest
	if !decodeJSONLimit(w, r, &input, 48<<10) {
		return
	}
	result, err := s.brainStage.Stage(r.Context(), projectbrain.StageInput{
		TaskID: input.TaskID, ValidationID: input.ValidationID,
		SelectedArtifactIDs: input.SelectedArtifactIDs, Solution: input.Solution,
		VerificationKind: input.VerificationKind, Approval: input.Approval,
	})
	if err != nil {
		projectBrainSelectionError(w, err)
		return
	}
	// The durable outbox is the authority for delivery. A temporary Pi outage
	// leaves the candidate pending and never changes the completed local task.
	deliveryCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	if _, err := s.brainOutbox.Drain(deliveryCtx, 1); err != nil {
		s.logger.Warn("Project Brain candidate delivery deferred", "reason", "outbox_unavailable")
	}
	if latest, err := s.brainOutbox.Get(r.Context(), result.CandidateID); err == nil {
		result = latest
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) getProjectBrainCandidate(w http.ResponseWriter, r *http.Request) {
	if !s.projectBrainConfigured(w) {
		return
	}
	result, err := s.brainOutbox.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Candidate not found"})
		return
	}
	actor := identity.Principal(r.Context())
	var owner string
	err = s.store.DB.QueryRowContext(r.Context(),
		`SELECT owner_principal_id FROM durable_tasks WHERE id=?`, result.TaskID).Scan(&owner)
	if err != nil || actor == "" || owner != actor {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Candidate not found"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
