package web

import (
	"database/sql"
	"errors"
	"net/http"

	"hermetrix-harness/internal/taskcoord"
)

func (s *Server) startTaskGoal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	var input taskcoord.StartGoalInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.TaskID = r.PathValue("id")
	goal, err := s.coord.StartGoal(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, goal)
}

func (s *Server) getGoal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	goal, err := s.tasks.Goal(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	actions, err := s.tasks.GoalActions(r.Context(), goal.ID, 50)
	if err != nil {
		taskError(w, err)
		return
	}
	result := map[string]any{"goal": goal, "actions": actions}
	x, err := s.tasks.Execution(r.Context(), goal.TaskID)
	if err != nil {
		taskError(w, err)
		return
	}
	if x.Proposal != nil {
		v, loadErr := s.tasks.GoalVerification(r.Context(), goal.ID, x.Proposal.ID)
		if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
			taskError(w, loadErr)
			return
		}
		if loadErr == nil {
			result["verification"] = v
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) listTaskGoals(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	items, err := s.tasks.GoalsForTask(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) stopGoal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	if s.coord == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task coordinator is unavailable"})
		return
	}
	goal, err := s.coord.StopGoal(r.Context(), r.PathValue("id"))
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, goal)
}

func (s *Server) resumeGoal(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input struct {
		ExpectedTaskRevision int `json:"expected_task_revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	goal, err := s.tasks.ResumeGoal(r.Context(), r.PathValue("id"), input.ExpectedTaskRevision)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, goal)
}
