package product

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChromeHeldGameKeyMovesAndReleasesWithExactApproval(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BROWSER_NETWORK_E2E") != "1" {
		t.Skip("enable real browser network E2E")
	}
	if _, err := chromeExecutable(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body><button id="game" aria-label="Game controls">Play</button><output id="state">distance=0 released=0</output><script>
let down=false,distance=0,released=0;window.addEventListener('keydown',e=>{if(e.key==='ArrowUp')down=true});window.addEventListener('keyup',e=>{if(e.key==='ArrowUp'){down=false;released++;document.querySelector('#state').textContent='distance='+distance+' released='+released}});function frame(){if(down)distance++;requestAnimationFrame(frame)}requestAnimationFrame(frame);</script></body></html>`)
	}))
	defer server.Close()
	s, _, _ := testProductService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	project, err := s.SaveProject(ctx, ProjectInput{Name: "Held key game fixture", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.OpenBrowserTab(ctx, OpenBrowserTabInput{ProjectID: project.ID, URL: server.URL, Actor: "test", AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.BrowserAction(context.Background(), page.ID, BrowserActionInput{Action: "close", Actor: "test"})
	var input BrowserActionInput
	json.Unmarshal([]byte(`{"action":"key","ref":1,"text":"ArrowUp","duration_ms":500,"actor":"test"}`), &input)
	input.ObservationHash = page.ObservationHash
	approval, err := s.PrepareBrowserAction(ctx, page.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.ReceiptID, input.SubjectHash = approval.ID, approval.SubjectHash
	var changed BrowserActionInput
	data, _ := json.Marshal(input)
	json.Unmarshal(data, &changed)
	json.Unmarshal([]byte(`{"duration_ms":1500}`), &changed)
	if _, err := s.BrowserAction(ctx, page.ID, changed); err == nil {
		t.Fatal("changed held duration accepted after exact approval")
	}
	result, err := s.BrowserAction(ctx, page.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	var distance, released int
	start := strings.Index(result.TextSnapshot, "distance=")
	if start < 0 {
		t.Fatal(result.TextSnapshot)
	}
	fmt.Sscanf(result.TextSnapshot[start:], "distance=%d released=%d", &distance, &released)
	if distance < 5 || released != 1 {
		t.Fatalf("held key did not move then release: %s", result.TextSnapshot)
	}
	if _, err := s.BrowserAction(ctx, page.ID, input); err == nil {
		t.Fatal("held action replayed")
	}
}
