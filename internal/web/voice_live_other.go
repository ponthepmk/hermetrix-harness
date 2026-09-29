//go:build !windows

package web

import "net/http"

func (s *Server) streamBrowserVoice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{"error": "live local voice is currently available on Windows"})
}
