package product

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"hermetrix-harness/internal/agentruntime"
)

const maxObservedScriptBytes = 512 << 10
const maxSourceExcerptBytes = 16 << 10

var clientPathLiteral = regexp.MustCompile(`['"](/[A-Za-z0-9][A-Za-z0-9/_-]{1,120})['"]`)

// readObservedBrowserScript uses Chrome's retained response body for a script
// this tab already loaded. It never asks the page to fetch the URL again.
func readObservedBrowserScript(ctx context.Context, client *cdpClient, rawURL string) (agentruntime.BrowserClientSource, error) {
	script, ok := client.observedScript(rawURL)
	if !ok {
		return agentruntime.BrowserClientSource{}, fmt.Errorf("source URL is not a completed script observed in this tab")
	}
	if script.size <= 0 || script.size > maxObservedScriptBytes {
		return agentruntime.BrowserClientSource{}, fmt.Errorf("observed script is empty or exceeds the 512 KiB source limit")
	}
	var response struct {
		Body          string `json:"body"`
		Base64Encoded bool   `json:"base64Encoded"`
	}
	if err := client.call(ctx, "Network.getResponseBody", map[string]any{"requestId": script.requestID}, &response); err != nil {
		return agentruntime.BrowserClientSource{}, fmt.Errorf("read observed script body: %w", err)
	}
	content := []byte(response.Body)
	if response.Base64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(response.Body)
		if err != nil {
			return agentruntime.BrowserClientSource{}, fmt.Errorf("decode observed script body: %w", err)
		}
		content = decoded
	}
	if len(content) > maxObservedScriptBytes {
		return agentruntime.BrowserClientSource{}, fmt.Errorf("decoded script exceeds the 512 KiB source limit")
	}
	whole := strings.ToValidUTF8(string(content), "�")
	hints := make([]string, 0, 40)
	seen := map[string]bool{}
	for _, match := range clientPathLiteral.FindAllStringSubmatch(whole, 200) {
		path := match[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		hints = append(hints, path)
		if len(hints) == 40 {
			break
		}
	}
	excerpt := whole
	truncated := len(excerpt) > maxSourceExcerptBytes
	if truncated {
		excerpt = excerpt[:maxSourceExcerptBytes]
		for !utf8.ValidString(excerpt) {
			excerpt = excerpt[:len(excerpt)-1]
		}
	}
	return agentruntime.BrowserClientSource{URL: script.url, Excerpt: excerpt, Truncated: truncated, PathHints: hints}, nil
}
