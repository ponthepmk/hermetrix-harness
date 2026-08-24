package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestErrorPagesDoNotBecomeConversation covers a gateway timeout whose 2 KiB
// Cloudflare error page became the turn_failed event, was replayed to the model
// as assistant history, and was carried into a compacted checkpoint -- markup
// and all, in budget meant for evidence.
func TestErrorPagesDoNotBecomeConversation(t *testing.T) {
	page := `<!DOCTYPE html>
<!--[if lt IE 7]> <html class="no-js ie6 oldie" lang="en-US"> <![endif]-->
<head><style>.cf-icon-browser{display:block}</style><title>Gateway time-out</title></head>
<body><h1>Error 524</h1><p>The origin web server timed out.</p>` +
		strings.Repeat(`<span class="cf-icon-browser block md:hidden h-20"></span>`, 200) +
		`</body></html>`
	summary := summariseErrorBody("text/html; charset=UTF-8", page)
	if strings.Contains(summary, "<") || strings.Contains(summary, "cf-icon-browser") {
		t.Fatalf("markup survived into the summary: %q", summary)
	}
	if !strings.Contains(summary, "524") || !strings.Contains(summary, "timed out") {
		t.Fatalf("the readable part was lost: %q", summary)
	}
	if runes := []rune(summary); len(runes) > maxErrorPageRunes+40 {
		t.Fatalf("summary is %d runes; an error page must not occupy conversation budget", len(runes))
	}
}

// TestProviderErrorBodiesAreKept keeps the useful case intact: providers return
// short structured errors and those are the diagnosis.
func TestProviderErrorBodiesAreKept(t *testing.T) {
	body := `{"error":{"message":"model qwen3.8-27b-fp8 is not available","type":"invalid_request_error"}}`
	summary := summariseErrorBody("application/json", body)
	if summary != body {
		t.Fatalf("a short JSON error was altered:\n got %q\nwant %q", summary, body)
	}
}

func TestHTMLIsDetectedWithoutAContentType(t *testing.T) {
	summary := summariseErrorBody("", "<html><body>Bad gateway</body></html>")
	if strings.Contains(summary, "<") {
		t.Fatalf("markup survived when the content type was missing: %q", summary)
	}
	if !strings.Contains(summary, "Bad gateway") {
		t.Fatalf("readable text was lost: %q", summary)
	}
}

func TestLongPlainErrorsAreBounded(t *testing.T) {
	summary := summariseErrorBody("text/plain", strings.Repeat("upstream connect error ", 500))
	if runes := []rune(summary); len(runes) > maxErrorBodyRunes+2 {
		t.Fatalf("plain body was not bounded: %d runes", len(runes))
	}
}

func TestAnEmptyBodyStillSaysSomething(t *testing.T) {
	if summary := summariseErrorBody("text/html", "   "); summary == "" {
		t.Fatal("an empty error body produced an empty summary")
	}
}

// TestAGatewayErrorPageNeverReachesTheCaller drives the HTTP path, not the
// helper beside it: this is where the Cloudflare page entered the conversation.
func TestAGatewayErrorPageNeverReachesTheCaller(t *testing.T) {
	page := `<!DOCTYPE html><head><style>.cf-icon{display:block}</style></head>` +
		`<body><h1>Error 524</h1><p>The origin web server timed out.</p>` +
		strings.Repeat(`<span class="cf-icon-browser"></span>`, 300) + `</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.WriteHeader(524)
		fmt.Fprint(w, page)
	}))
	defer server.Close()
	_, err := NewOpenAIAdapter(server.Client()).StreamChat(context.Background(),
		Profile{BaseURL: server.URL, Model: "m"}, "secret-token",
		ChatRequest{Messages: []Message{{Role: "user", Content: "ping"}}, MaxTokens: 16}, nil)
	if err == nil {
		t.Fatal("a 524 did not produce an error")
	}
	message := err.Error()
	if strings.Contains(message, "<") || strings.Contains(message, "cf-icon") {
		t.Fatalf("markup reached the caller and would become conversation history: %q", message)
	}
	if !strings.Contains(message, "524") {
		t.Fatalf("the status code was lost: %q", message)
	}
	if strings.Contains(message, "secret-token") {
		t.Fatalf("the credential leaked into the error: %q", message)
	}
	if runes := []rune(message); len(runes) > maxErrorPageRunes+80 {
		t.Fatalf("error is %d runes; a page must not occupy conversation budget", len(runes))
	}
}
