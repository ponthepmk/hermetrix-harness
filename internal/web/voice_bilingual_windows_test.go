//go:build windows

package web

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in test with a local code-switching fixture and the resident Whisper
// worker. It checks the spoken scripts survive the real STT request.
func TestResidentWhisperPreservesThaiAndEnglish(t *testing.T) {
	fixture := os.Getenv("HERMETRIX_E2E_MIXED_WAV")
	endpoint := os.Getenv("HERMETRIX_WHISPER_SERVER")
	if fixture == "" || endpoint == "" {
		t.Skip("mixed WAV fixture and local Whisper server required")
	}
	wav, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transcript, _, err := transcribeResidentWhisper(ctx, endpoint, wav)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transcript, "สวัสดี") || !strings.Contains(strings.ToLower(transcript), "workspace") {
		t.Fatalf("mixed-language transcript lost Thai or English words: %q", transcript)
	}
}
