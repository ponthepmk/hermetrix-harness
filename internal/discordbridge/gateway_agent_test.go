package discordbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/secrets"
)

// Exercise the complete transport -> real session/tool authority -> Discord
// receipt path, rather than testing protocol and AgentAdapter separately.
func TestGatewayRealAgentExactApprovalAndDenial(t *testing.T) {
	for _, decision := range []string{"approve", "deny"} {
		t.Run(decision, func(t *testing.T) {
			f := newAdapterFixture(t, func(w http.ResponseWriter, _ *http.Request, call int32) {
				if call == 1 {
					writeAdapterApproval(w)
					return
				}
				writeAdapterAnswer(w, "The tool decision was processed.")
			})
			vault, err := secrets.Open(f.store.Root)
			if err != nil {
				t.Fatal(err)
			}
			bridge, err := NewService(context.Background(), f.store, vault, f.adapter)
			if err != nil {
				t.Fatal(err)
			}
			rest := &bridgeRESTFixture{}
			restServer := httptest.NewServer(http.HandlerFunc(rest.serve))
			defer restServer.Close()
			cfg := Config{ApplicationID: testApp, GuildIDs: []string{testGuild}, ChannelIDs: []string{testChannel}, UserIDs: []string{testUser}, ProjectID: f.binding.ProjectID, ProviderID: f.binding.ProviderID, ContextProfile: f.binding.ContextProfile}
			bridge.apiBase = restServer.URL
			bridge.allowTestEndpoints = true
			if _, err := bridge.Configure(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
			if _, err := bridge.SetToken(context.Background(), testBotToken); err != nil {
				t.Fatal(err)
			}
			ask := testInteraction("ask", "Create approved.txt only after I approve its exact content")
			var gatewayServer *httptest.Server
			gatewayServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
				// This test verifies approval/denial, not heartbeat deadlines. A
				// 20ms deadline spuriously disconnects under the full suite's load.
				_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 200}})
				var identify gatewayPacket
				if conn.ReadJSON(&identify) != nil || identify.Op != 2 {
					return
				}
				_ = conn.WriteJSON(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{"session_id": "real-agent-fixture", "resume_gateway_url": "ws" + strings.TrimPrefix(gatewayServer.URL, "http"), "application": map[string]string{"id": testApp}, "user": map[string]string{"id": "999999999999999999"}}})
				for _, seq := range []int{2, 3} {
					_ = conn.WriteJSON(map[string]any{"op": 0, "s": seq, "t": "INTERACTION_CREATE", "d": ask})
				}
				sentDecision := false
				for {
					var packet gatewayPacket
					if conn.ReadJSON(&packet) != nil {
						return
					}
					if packet.Op != 1 {
						continue
					}
					_ = conn.WriteJSON(map[string]any{"op": 11})
					if sentDecision {
						continue
					}
					bridge.mu.Lock()
					busy := len(bridge.jobs) != 0
					bridge.mu.Unlock()
					if busy {
						continue
					}
					var approvalID string
					if f.store.DB.QueryRow(`SELECT approval_id FROM discord_bridge_approvals WHERE binding_key=?`, bindingKey(testGuild, testChannel, testUser)).Scan(&approvalID) != nil {
						continue
					}
					if _, err := os.Stat(filepath.Join(f.root, "approved.txt")); !os.IsNotExist(err) {
						t.Error("file changed before Discord approval")
					}
					sentDecision = true
					_ = conn.WriteJSON(map[string]any{"op": 0, "s": 4, "t": "INTERACTION_CREATE", "d": testInteraction(decision, approvalID)})
				}
			}))
			defer gatewayServer.Close()
			defer bridge.Close() // Stop socket/jobs before fixture HTTP cleanup.
			rest.gatewayURL = "ws" + strings.TrimPrefix(gatewayServer.URL, "http")
			if err := bridge.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(8 * time.Second)
			var state string
			for time.Now().Before(deadline) {
				_ = f.store.DB.QueryRow(`SELECT state FROM discord_bridge_interactions WHERE command=?`, decision).Scan(&state)
				if state == "completed" || state == "failed" || state == "reply_failed" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if state != "completed" {
				status, _ := bridge.Status(context.Background())
				t.Fatalf("Discord decision state=%q gateway=%s error=%s", state, status.State, status.LastError)
			}
			if f.calls.Load() != 2 {
				t.Fatalf("duplicate interaction repeated inference: calls=%d", f.calls.Load())
			}
			content, err := os.ReadFile(filepath.Join(f.root, "approved.txt"))
			if decision == "approve" {
				if err != nil || string(content) != "exact reviewed content" {
					t.Fatalf("approved content=%q err=%v", content, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("denied file exists: %v", err)
			}
			rest.mu.Lock()
			var replies []string
			for _, req := range rest.requests {
				if req["method"] == http.MethodPatch {
					replies = append(replies, fmt.Sprint(req["body"].(map[string]any)["content"]))
				}
				if strings.HasSuffix(fmt.Sprint(req["path"]), "/callback") {
					encoded, _ := json.Marshal(req["body"])
					if !strings.Contains(string(encoded), `"flags":64`) {
						t.Error("Discord acknowledgement was not ephemeral")
					}
				}
			}
			rest.mu.Unlock()
			if len(replies) != 2 || !strings.Contains(replies[0], "exact reviewed content") || !strings.Contains(replies[1], "The tool decision was processed.") {
				t.Fatalf("Discord receipts=%q", replies)
			}
		})
	}
}
