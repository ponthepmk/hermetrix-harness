//go:build !windows

package web

import "net/http"

func (s *Server) voiceDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ไมโครโฟนของระบบปฏิบัติการยังรองรับเฉพาะ Windows"})
}

func (s *Server) streamNativeVoice(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "ไมโครโฟนของระบบปฏิบัติการยังรองรับเฉพาะ Windows"})
}
