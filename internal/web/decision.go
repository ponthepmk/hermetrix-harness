package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"hermetrix-harness/internal/taskengine"
)

func (s *Server) listDecisionFixtures(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"fixtures": taskengine.DecisionFixtures(),
		"threshold": taskcoordThreshold()})
}

func taskcoordThreshold() any {
	return taskengine.CurrentDecisionAdmissionThreshold()
}

func (s *Server) listDecisionBenchmarks(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.tasks.ListDecisionBenchmarks(r.Context(), r.URL.Query().Get("provider_id"), limit)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) runDecisionBenchmark(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		ProviderID string `json:"provider_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	run, err := s.coord.RunDecisionBenchmark(r.Context(), input.ProviderID)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) getDecisionAdmission(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var policy taskengine.DecisionAdmissionPolicy
	var err error
	if providerID := r.URL.Query().Get("provider_id"); providerID != "" {
		policy, err = s.tasks.DecisionAdmission(r.Context(), providerID)
	} else {
		policy, err = s.tasks.LatestEnabledDecisionAdmission(r.Context())
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "threshold": taskcoordThreshold()})
		return
	}
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "threshold": taskcoordThreshold()})
}

func (s *Server) setDecisionAdmission(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		ProviderID     string `json:"provider_id"`
		BenchmarkRunID string `json:"benchmark_run_id"`
		Actor          string `json:"actor"`
		Reason         string `json:"reason"`
		Enabled        bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	policy, err := s.coord.SetDecisionAdmission(r.Context(), input.ProviderID, input.BenchmarkRunID,
		input.Actor, input.Reason, input.Enabled)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) decideTaskReadOnly(w http.ResponseWriter, r *http.Request) {
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input struct {
		ExpectedTaskRevision int `json:"expected_task_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.coord.DecideAdmittedReadOnly(r.Context(), r.PathValue("id"), input.ExpectedTaskRevision)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
