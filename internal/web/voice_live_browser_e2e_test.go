//go:build windows

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Uses a fixture microphone and the resident local Whisper server. It is
// opt-in because normal CI has neither a model nor GPU and should not touch a
// person's live microphone.
func TestLiveVoicePartialAndFinalInARealBrowser(t *testing.T) {
	wav := os.Getenv("HERMETRIX_E2E_FAKE_MIC_WAV")
	if wav == "" || os.Getenv("HERMETRIX_WHISPER_SERVER") == "" {
		t.Skip("fake WAV and local Whisper server required")
	}
	if _, err := os.Stat(wav); err != nil {
		t.Fatal(err)
	}
	transcriptNeedle := os.Getenv("HERMETRIX_E2E_EXPECTED_TRANSCRIPT")
	if transcriptNeedle == "" {
		transcriptNeedle = "Americans"
	}
	transcriptNeedles, err := json.Marshal(strings.Split(strings.ToLower(transcriptNeedle), "|"))
	if err != nil {
		t.Fatal(err)
	}
	chrome := browserExecutableForE2E()
	if chrome == "" {
		t.Skip("Chrome is not installed")
	}
	server := testHTTPServer(t)
	modelURL, modelID, replyNeedle, outputCap := "", "voice-fixture", "Fixture voice reply.", 256
	if realURL := strings.TrimSpace(os.Getenv("HERMETRIX_E2E_REAL_LLM_URL")); realURL != "" {
		if realURL != "http://127.0.0.1:8088/v1" {
			t.Fatal("real model test requires the local Bonsai endpoint")
		}
		response, err := http.Get(realURL + "/models")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var listing struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&listing); err != nil || len(listing.Data) == 0 {
			t.Fatalf("Bonsai model unavailable: %v", err)
		}
		modelURL, modelID, replyNeedle, outputCap = realURL, listing.Data[0].ID, "", 2048
	} else {
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Fixture voice reply.\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		defer model.Close()
		modelURL = model.URL + "/v1"
	}
	requestJSON(t, server.URL+"/api/providers", http.MethodPost, map[string]any{
		"name": "Voice browser test", "adapter_kind": "openai-compatible", "base_url": modelURL,
		"model": modelID, "context_window": 65536, "context_evidence": "declared", "max_output_tokens": outputCap,
	}, http.StatusCreated)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	debugURL, stop, err := startE2EChrome(ctx, chrome, filepath.Join(t.TempDir(), "chrome-profile"))
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	target, err := createE2ETarget(ctx, debugURL, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := &e2eCDPClient{connection: connection}
	if err := client.call(ctx, "Runtime.enable", map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#appShell:not([hidden]) .voice-avatar[data-voice-open]') ? 'ready' : ''`, "ready")
	if _, err := client.evaluate(ctx, `window.__voiceTrail=[];window.__orbTrail=[];new MutationObserver(()=>window.__voiceTrail.push(document.querySelector('#voiceStatus').textContent)).observe(document.querySelector('#voiceStatus'),{childList:true,characterData:true,subtree:true});new MutationObserver(()=>window.__orbTrail.push(document.querySelector('#voicePanel').dataset.voiceState)).observe(document.querySelector('#voicePanel'),{attributes:true,attributeFilter:['data-voice-state']});document.querySelector('.voice-avatar[data-voice-open]').click();'opened'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#view-chat .voice-host > #voicePanel:not([hidden]) #voiceOrb img')?.complete && !document.querySelector('#zoneSide #voicePanel') ? 'voice-in-chat' : ''`, "voice-in-chat")
	waitVoiceBrowserValue(t, ctx, client, fmt.Sprintf(`window.__voiceTrail.some(s=>s.includes('คำพูดระหว่างพูด')) && window.__voiceTrail.some(s=>s.includes('ถอดเสียงแล้ว')) && %s.every(s=>document.querySelector('#voiceTranscript')?.value.toLowerCase().includes(s)) ? 'live-final' : JSON.stringify({transcript:document.querySelector('#voiceTranscript')?.value,turns:[...document.querySelectorAll('.chat-message.user .message-body')].map(n=>n.textContent),trail:window.__voiceTrail})`, transcriptNeedles), "live-final")
	if _, err := client.evaluate(ctx, `document.querySelector('#voiceMic').click(); 'mic-stopped'`); err != nil {
		t.Fatal(err)
	}
	waitVoiceBrowserValue(t, ctx, client, fmt.Sprintf(`state.sessionDetail?.session?.id && !state.sessionDetail.session.project_id && document.querySelector('#voiceAutoSend')?.checked && [...document.querySelectorAll('.chat-message.assistant .message-body')].some(node=>node.textContent.trim().length>0 && node.textContent.includes(%q)) ? 'auto-replied' : JSON.stringify({session:state.sessionDetail?.session?.id,auto:document.querySelector('#voiceAutoSend')?.checked,trail:window.__voiceTrail.slice(-5)})`, replyNeedle), "auto-replied")
	waitBrowserValue(t, ctx, client, `window.__voiceTrail.some(s=>s.includes('กำลังพูดตอบ')) ? 'tts-playing' : JSON.stringify(window.__voiceTrail.slice(-8))`, "tts-playing")
	waitBrowserValue(t, ctx, client, `window.__orbTrail.includes('listening') && window.__orbTrail.includes('speaking') ? 'orb-reacted' : JSON.stringify(window.__orbTrail)`, "orb-reacted")
	if _, err := client.evaluate(ctx, `document.querySelector('#voiceEnd').click(); 'closing'`); err != nil {
		t.Fatal(err)
	}
	waitBrowserValue(t, ctx, client, `document.querySelector('#voicePanel')?.hidden && !document.querySelector('#view-chat #voicePanel') ? 'voice-closed' : ''`, "voice-closed")
}

func waitVoiceBrowserValue(t *testing.T, ctx context.Context, client *e2eCDPClient, expression, want string) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	var value string
	var err error
	for time.Now().Before(deadline) {
		value, err = client.evaluate(ctx, expression)
		if err == nil && value == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("browser voice value did not become %q: value=%q err=%v", want, value, err)
}
