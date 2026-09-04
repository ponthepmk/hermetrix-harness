package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestBrowserRequestGuardRejectsPrivateNetworkSchemesBeforeRelease(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1/admin",
		"https://169.254.169.254/latest/meta-data",
		"ws://10.0.0.8/socket",
		"wss://[::1]/socket",
		"ftp://203.0.113.10/file",
	} {
		if err := service.validateBrowserRequestURL(ctx, "", raw, false); err == nil {
			t.Errorf("request guard accepted %s", raw)
		}
	}
	for _, raw := range []string{"about:blank", "data:text/plain,ok", "blob:https://example.com/id"} {
		if err := service.validateBrowserRequestURL(ctx, "", raw, false); err != nil {
			t.Errorf("request guard rejected local non-network URL %s: %v", raw, err)
		}
	}
}

func TestCDPFetchPauseIsFailedBeforeTheOriginalCommandCompletes(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverResult <- err
			return
		}
		defer connection.Close()
		var command struct {
			ID int64 `json:"id"`
		}
		if err := connection.ReadJSON(&command); err != nil {
			serverResult <- err
			return
		}
		if err := connection.WriteJSON(map[string]any{"method": "Fetch.requestPaused", "params": map[string]any{
			"requestId": "paused-private", "request": map[string]any{"url": "http://127.0.0.1/admin"},
		}}); err != nil {
			serverResult <- err
			return
		}
		var decision struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := connection.ReadJSON(&decision); err != nil {
			serverResult <- err
			return
		}
		if decision.Method != "Fetch.failRequest" || decision.Params["requestId"] != "paused-private" ||
			decision.Params["errorReason"] != "BlockedByClient" {
			serverResult <- fmt.Errorf("unexpected Fetch decision: %+v", decision)
			return
		}
		if err := connection.WriteJSON(map[string]any{"id": decision.ID, "result": map[string]any{}}); err != nil {
			serverResult <- err
			return
		}
		if err := connection.WriteJSON(map[string]any{"id": command.ID, "result": json.RawMessage(`{}`)}); err != nil {
			serverResult <- err
			return
		}
		serverResult <- nil
	}))
	defer server.Close()

	connection, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := &cdpClient{conn: connection, requestGuard: func(_ context.Context, raw string) error {
		if strings.Contains(raw, "127.0.0.1") {
			return errors.New("private destination")
		}
		return nil
	}}
	err = client.call(context.Background(), "Page.navigate", map[string]any{"url": "https://public.example"}, nil)
	var policyErr *browserRequestPolicyError
	if !errors.As(err, &policyErr) {
		t.Fatalf("call error = %v, want browserRequestPolicyError", err)
	}
	if serverErr := <-serverResult; serverErr != nil {
		t.Fatal(serverErr)
	}
}
