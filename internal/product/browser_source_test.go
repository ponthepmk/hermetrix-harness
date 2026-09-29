package product

import (
	"context"
	"strings"
	"testing"
)

func TestSourceRefusesUnobservedScriptWithoutNetworkCall(t *testing.T) {
	client := &cdpClient{}
	_, err := readObservedBrowserScript(context.Background(), client, "https://example.test/secret.js")
	if err == nil || !strings.Contains(err.Error(), "not a completed script") {
		t.Fatalf("unobserved script was accepted: %v", err)
	}
}

func TestClientPathLiteralFindsPathsOnly(t *testing.T) {
	matches := clientPathLiteral.FindAllStringSubmatch(`const a="/api/items",b='/auth/me',c="/api/token?secret=x"`, -1)
	if len(matches) != 2 || matches[0][1] != "/api/items" || matches[1][1] != "/auth/me" {
		t.Fatalf("path matches: %+v", matches)
	}
}
