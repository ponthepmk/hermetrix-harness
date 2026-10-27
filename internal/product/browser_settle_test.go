package product

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChromeApprovedClickObservesDelayedSPARouteWithoutReplaying(t *testing.T) {
	if os.Getenv("HERMETRIX_RUN_BROWSER_NETWORK_E2E") != "1" {
		t.Skip("enable real browser network E2E")
	}
	if _, err := chromeExecutable(); err != nil {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><button onclick="this.disabled=true;window.clicks=(window.clicks||0)+1;setTimeout(()=>{history.pushState({},'', '/shop');document.body.innerHTML='<p>SHOP-READY clicks='+window.clicks+'</p>';},350)">Shop</button></body></html>`)
	}))
	defer server.Close()
	s, _, _ := testProductService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	project, err := s.SaveProject(ctx, ProjectInput{Name: "SPA fixture", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.OpenBrowserTab(ctx, OpenBrowserTabInput{ProjectID: project.ID, URL: server.URL, Actor: "test", AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.BrowserAction(context.Background(), page.ID, BrowserActionInput{Action: "close", Actor: "test"})
	ref := 0
	for _, element := range page.Elements {
		if element.Text == "Shop" {
			ref = element.Ref
		}
	}
	if ref == 0 {
		t.Fatal("button missing")
	}
	input := BrowserActionInput{Action: "click", Ref: ref, ObservationHash: page.ObservationHash, Actor: "test"}
	approval, err := s.PrepareBrowserAction(ctx, page.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	input.ReceiptID, input.SubjectHash = approval.ID, approval.SubjectHash
	result, err := s.BrowserAction(ctx, page.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != server.URL+"/shop" || !strings.Contains(result.TextSnapshot, "SHOP-READY clicks=1") {
		t.Fatalf("async route not observed: %s %s", result.URL, result.TextSnapshot)
	}
	if _, err = s.BrowserAction(ctx, page.ID, input); err == nil {
		t.Fatal("one-use click approval was replayed")
	}
}
