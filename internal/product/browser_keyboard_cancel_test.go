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

func TestChromeHeldKeyCancellationReleasesWithoutReplaying(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BROWSER_NETWORK_E2E") != "1" {
		t.Skip("enable real browser network E2E")
	}
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/started" {
			select {
			case started <- struct{}{}:
			default:
			}
			fmt.Fprint(w, "ok")
			return
		}
		fmt.Fprint(w, `<html><body><canvas tabindex="0" role="application" aria-label="Game"></canvas><output id="state">held=false released=0</output><script>let up=0;window.addEventListener('keydown',e=>{if(e.key==='w'){document.querySelector('#state').textContent='held=true released='+up;fetch('/started')}});window.addEventListener('keyup',e=>{if(e.key==='w'){up++;document.querySelector('#state').textContent='held=false released='+up}})</script></body></html>`)
	}))
	defer server.Close()
	s, _, _ := testProductService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	project, err := s.SaveProject(ctx, ProjectInput{Name: "Cancel game key", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.OpenBrowserTab(ctx, OpenBrowserTabInput{ProjectID: project.ID, URL: server.URL, Actor: "test", AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.BrowserAction(context.Background(), page.ID, BrowserActionInput{Action: "close", Actor: "test"})
	var input BrowserActionInput
	json.Unmarshal([]byte(`{"action":"key","ref":1,"text":"w","duration_ms":3000,"actor":"test"}`), &input)
	input.ObservationHash = page.ObservationHash
	approval, err := s.PrepareBrowserAction(ctx, page.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.ReceiptID, input.SubjectHash = approval.ID, approval.SubjectHash
	actionCtx, cancelAction := context.WithCancel(ctx)
	defer cancelAction()
	finished := make(chan error, 1)
	go func() { _, err := s.BrowserAction(actionCtx, page.ID, input); finished <- err }()
	select {
	case <-started:
		cancelAction()
	case <-time.After(4 * time.Second):
		t.Fatal("keyDown never reached canvas")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled action claimed success")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("cancel did not stop held key")
	}
	read, err := s.BrowserAction(ctx, page.ID, BrowserActionInput{Action: "read", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read.TextSnapshot, "held=false released=1") {
		t.Fatalf("cancel left a key down: %s", read.TextSnapshot)
	}
	receipt, err := s.GetBrowserActionReceipt(ctx, approval.ID)
	if err != nil || receipt.State != "uncertain" {
		t.Fatalf("cancel lost uncertain-effect evidence: %+v %v", receipt, err)
	}
	if _, err := s.BrowserAction(ctx, page.ID, input); err == nil {
		t.Fatal("cancelled input replayed")
	}
	input.ObservationHash = read.ObservationHash
	if _, err := s.PrepareBrowserAction(ctx, page.ID, input); err == nil {
		t.Fatal("uncertain action allowed an automatic subsequent input")
	}
}
