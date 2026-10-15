package web

import (
	"net/http"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/providers"
)

func (s *Server) confirmProviderIdle(w http.ResponseWriter, r *http.Request) {
	if s.providers == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "provider service is unavailable"})
		return
	}
	profile, err := s.providers.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if !providers.IsLocalProfile(profile) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "idle confirmation only supports local runtimes"})
		return
	}
	if err = s.providers.ConfirmRuntimeIdle(r.Context(), profile); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtime_idle": true, "provider_id": profile.ID})
}

func (s *Server) stopSessionTurn(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input struct {
		TurnID string `json:"turn_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.agent.StopTurn(r.Context(), r.PathValue("id"), input.TurnID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) queueSessionMessage(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input agent.QueueInput
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.agent.QueueMessage(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) reconcileSessionStop(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	var input struct {
		TurnID string `json:"turn_id"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := s.agent.ReconcileStoppedTurn(r.Context(), r.PathValue("id"), input.TurnID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) cancelSessionMessage(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "agent service is unavailable"})
		return
	}
	item, err := s.agent.CancelQueuedMessage(r.Context(), r.PathValue("id"), r.PathValue("message"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
