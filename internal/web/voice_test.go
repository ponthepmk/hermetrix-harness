package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestVoiceRejectsUntrustedAudioType(t *testing.T) {
	if _, err := findLocalWhisper(); err != nil {
		t.Skip(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", strings.NewReader("not audio"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	(&Server{}).transcribeVoice(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
}

func TestNativeVoiceRejectsUnlistedDevice(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows microphone capture is required")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/voice/native/stream", strings.NewReader(`{"device":"-ar 9999"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	(&Server{}).streamNativeVoice(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
}

func TestLocalVoiceSpeechRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows speech synthesis is required")
	}
	if _, err := findLocalWhisper(); err != nil {
		t.Skip(err)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip(err)
	}
	server := &Server{}
	speakReq := httptest.NewRequest(http.MethodPost, "/api/voice/speak", strings.NewReader(`{"text":"สวัสดีครับ ตอนนี้ทดสอบเสียงในเครื่อง"}`))
	speakReq.Header.Set("Content-Type", "application/json")
	speak := httptest.NewRecorder()
	server.speakVoice(speak, speakReq)
	if speak.Code != http.StatusOK || !bytes.HasPrefix(speak.Body.Bytes(), []byte("RIFF")) {
		t.Fatalf("local speech failed: status=%d body=%q", speak.Code, speak.Body.String())
	}
	t.Logf("generated WAV bytes: %d", speak.Body.Len())
	transcribeReq := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", bytes.NewReader(speak.Body.Bytes()))
	transcribeReq.Header.Set("Content-Type", "audio/wav")
	transcribe := httptest.NewRecorder()
	server.transcribeVoice(transcribe, transcribeReq)
	if transcribe.Code != http.StatusOK {
		t.Fatalf("local transcription failed: status=%d body=%s", transcribe.Code, transcribe.Body.String())
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(transcribe.Body.Bytes(), &result); err != nil || strings.TrimSpace(result.Text) == "" {
		t.Fatalf("transcription is empty or invalid: %v %s", err, transcribe.Body.String())
	}
}
