package web

import (
	"hermetrix-harness/internal/taskengine"
	"net/http"
)

func (s *Server) pauseIdleTaskRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireTaskEngine(w) {
		return
	}
	var input taskengine.PauseRunInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.RunID = r.PathValue("id")
	input.Actor = effectiveActor(r.Context(), input.Actor)
	task, err := s.tasks.PauseIdleRun(r.Context(), input)
	if err != nil {
		taskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}
