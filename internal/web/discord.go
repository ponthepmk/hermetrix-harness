package web

import (
	"errors"
	"net/http"

	"hermetrix-harness/internal/discordbridge"
)

func writeDiscordError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, discordbridge.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Discord configuration belongs to another user."})
	case errors.Is(err, discordbridge.ErrInvalidConfig):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeError(w, err)
	}
}

// The browser configures a local outbound bridge. No public Discord webhook,
// bot token, or unauthenticated command endpoint is exposed by this server.
func (s *Server) registerDiscordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/remote/discord", s.discordStatus)
	mux.HandleFunc("PUT /api/remote/discord", s.discordConfigure)
	mux.HandleFunc("PUT /api/remote/discord/token", s.discordToken)
	mux.HandleFunc("POST /api/remote/discord/start", s.discordStart)
	mux.HandleFunc("POST /api/remote/discord/stop", s.discordStop)
}

func (s *Server) requireDiscord(w http.ResponseWriter) bool {
	if s.discord != nil {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Discord remote control is unavailable in this server. Restart with the current build."})
	return false
}

func (s *Server) discordStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireDiscord(w) {
		return
	}
	result, err := s.discord.Status(r.Context())
	if err != nil {
		writeDiscordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) discordConfigure(w http.ResponseWriter, r *http.Request) {
	if !s.requireDiscord(w) {
		return
	}
	var input discordbridge.Config
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.discord.Configure(r.Context(), input)
	if err != nil {
		writeDiscordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) discordToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireDiscord(w) {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := s.discord.SetToken(r.Context(), input.Token)
	if err != nil {
		writeDiscordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) discordStart(w http.ResponseWriter, r *http.Request) {
	if !s.requireDiscord(w) {
		return
	}
	if err := s.discord.Start(r.Context()); err != nil {
		writeDiscordError(w, err)
		return
	}
	s.discordStatus(w, r)
}

func (s *Server) discordStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireDiscord(w) {
		return
	}
	if err := s.discord.Stop(r.Context()); err != nil {
		writeDiscordError(w, err)
		return
	}
	s.discordStatus(w, r)
}
