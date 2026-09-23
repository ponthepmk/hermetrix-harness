package discordbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
)

const testApp = "111111111111111111"
const testGuild = "222222222222222222"
const testChannel = "333333333333333333"
const testUser = "444444444444444444"
const testBotToken = "fixture-bot-token-never-real"

type fakeBridgeBackend struct {
	mu                            sync.Mutex
	newCalls, askCalls, decisions int
	pending                       *ApprovalSummary
	lastActor                     string
	block                         bool
}

func (f *fakeBridgeBackend) Validate(context.Context, Binding) error { return nil }
func (f *fakeBridgeBackend) New(_ context.Context, _ Binding, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newCalls++
	return fmt.Sprintf("session-%d", f.newCalls), nil
}
func (f *fakeBridgeBackend) Ask(ctx context.Context, _ Binding, session, text string) (Reply, error) {
	f.mu.Lock()
	f.askCalls++
	pending := f.pending
	block := f.block
	if pending != nil {
		copy := *pending
		copy.SessionID = session
		pending = &copy
		f.pending = pending
	}
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return Reply{}, ctx.Err()
	}
	return Reply{Text: "Answer: " + text, Approval: pending}, nil
}
func (f *fakeBridgeBackend) Status(_ context.Context, _ Binding, _ string) (Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Reply{Text: "Ready", Approval: f.pending}, nil
}
func (f *fakeBridgeBackend) GetApproval(_ context.Context, _ Binding, session, id string) (ApprovalSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil || f.pending.ID != id || f.pending.SessionID != session {
		return ApprovalSummary{}, errors.New("pending approval not found")
	}
	return *f.pending, nil
}
func (f *fakeBridgeBackend) Decide(_ context.Context, _ Binding, _ string, _ string, _ string, actor string) (Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions++
	f.lastActor = actor
	f.pending = nil
	return Reply{Text: "Applied"}, nil
}
func (f *fakeBridgeBackend) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.newCalls, f.askCalls, f.decisions
}

type bridgeRESTFixture struct {
	mu         sync.Mutex
	requests   []map[string]any
	ackStatus  int
	gatewayURL string
	globalRate bool
}

func (f *bridgeRESTFixture) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, map[string]any{"path": r.URL.Path, "method": r.Method, "body": body, "authorization": r.Header.Get("Authorization")})
	ackStatus := f.ackStatus
	gateway := f.gatewayURL
	rate := f.globalRate
	f.globalRate = false
	f.mu.Unlock()
	if rate {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"global":true,"retry_after":0.05}`)
		return
	}
	if r.URL.Path == "/gateway/bot" {
		_ = json.NewEncoder(w).Encode(map[string]any{"url": gateway, "session_start_limit": map[string]int{"remaining": 10}})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/callback") {
		if ackStatus == 0 {
			ackStatus = 204
		}
		w.WriteHeader(ackStatus)
		return
	}
	fmt.Fprint(w, `{}`)
}
func testBridge(t *testing.T) (*Service, *fakeBridgeBackend, *bridgeRESTFixture, Config, string) {
	t.Helper()
	ctx := context.Background()
	data, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	vault, err := secrets.Open(data.Root)
	if err != nil {
		t.Fatal(err)
	}
	backend := &fakeBridgeBackend{}
	s, err := NewService(ctx, data, vault, backend)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	rest := &bridgeRESTFixture{}
	server := httptest.NewServer(http.HandlerFunc(rest.serve))
	t.Cleanup(server.Close)
	s.apiBase = server.URL
	s.allowTestEndpoints = true
	s.reconnectDelay = 10 * time.Millisecond
	cfg := Config{ApplicationID: testApp, GuildIDs: []string{testGuild}, ChannelIDs: []string{testChannel}, UserIDs: []string{testUser}, ProjectID: "project-fixture", ProviderID: "local-fixture", ContextProfile: "compact-32k"}
	if _, err := s.Configure(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetToken(ctx, testBotToken); err != nil {
		t.Fatal(err)
	}
	owner, err := data.LocalPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s, backend, rest, cfg, owner
}

var snowflakeSequence atomic.Uint64

func testInteraction(command, arg string) interaction {
	var event interaction
	event.ID = strconv.FormatUint(uint64(time.Now().UnixMilli()-1420070400000)<<22|(snowflakeSequence.Add(1)&0x3fffff), 10)
	event.ApplicationID = testApp
	event.Type = 2
	event.Token = "fixture-interaction-token-" + event.ID
	event.GuildID = testGuild
	event.ChannelID = testChannel
	event.Member = &struct {
		User discordUser `json:"user"`
	}{User: discordUser{ID: testUser}}
	event.Data.Name = "hermetrix"
	event.Data.Type = 1
	option := interactionOption{Name: command, Type: 1}
	if arg != "" {
		name := "approval_id"
		if command == "ask" {
			name = "prompt"
		}
		value, _ := json.Marshal(arg)
		option.Options = []interactionOption{{Name: name, Type: 3, Value: value}}
	}
	event.Data.Options = []interactionOption{option}
	return event
}
func handleAndWait(s *Service, cfg Config, owner string, event interaction) {
	s.handleInteraction(context.Background(), cfg, owner, event)
	s.jobsWG.Wait()
}

func TestBridgeAllowlistsReplayPersistenceAndNoSecretDisclosure(t *testing.T) {
	s, backend, rest, cfg, owner := testBridge(t)
	for _, mutate := range []func(*interaction){func(e *interaction) { e.GuildID = "" }, func(e *interaction) { e.ChannelID = "555555555555555555" }, func(e *interaction) { e.Member.User.ID = "666666666666666666" }, func(e *interaction) { e.Member.User.Bot = true }, func(e *interaction) { e.ApplicationID = "777777777777777777" }} {
		event := testInteraction("ask", "denied")
		mutate(&event)
		handleAndWait(s, cfg, owner, event)
	}
	if _, ask, _ := backend.counts(); ask != 0 {
		t.Fatal("unauthorized interaction reached model")
	}
	event := testInteraction("ask", "read only fixture")
	handleAndWait(s, cfg, owner, event)
	handleAndWait(s, cfg, owner, event)
	s.Close()
	restarted, err := NewService(context.Background(), s.store, s.vault, backend)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	restarted.apiBase = s.apiBase
	handleAndWait(restarted, cfg, owner, event)
	handleAndWait(restarted, cfg, owner, testInteraction("ask", "continued"))
	newCalls, askCalls, _ := backend.counts()
	if newCalls != 1 || askCalls != 2 {
		t.Fatalf("persistent session/replay failed: new=%d ask=%d", newCalls, askCalls)
	}
	status, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), testBotToken) || !status.TokenStored {
		t.Fatalf("token status unsafe: %s", raw)
	}
	var stored string
	if err := s.store.DB.QueryRow(`SELECT config_json FROM discord_bridge_config`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "token") {
		t.Fatal("token entered database config")
	}
	rest.mu.Lock()
	defer rest.mu.Unlock()
	for _, request := range rest.requests {
		route := request["path"].(string)
		if strings.Contains(route, "/callback") || strings.Contains(route, "/webhooks/") {
			if request["authorization"] != "" {
				t.Fatal("bot credential sent to interaction endpoint")
			}
			body := request["body"].(map[string]any)
			if data, ok := body["data"].(map[string]any); ok {
				if data["flags"] != float64(64) {
					t.Fatal("reply is not ephemeral")
				}
				body = data
			}
			mentions, ok := body["allowed_mentions"].(map[string]any)
			if !ok || len(mentions["parse"].([]any)) != 0 {
				t.Fatal("reply may mention users")
			}
		}
	}
}

func TestBridgeApprovalRequiresDeliveredExactUserSessionAndHash(t *testing.T) {
	s, backend, _, cfg, owner := testBridge(t)
	cfg.UserIDs = append(cfg.UserIDs, "888888888888888888")
	backend.pending = &ApprovalSummary{ID: "approval-fixture", ToolName: "workspace.write_file", Summary: "replace fixture.txt", Preview: "reviewed content", ArgumentsHash: strings.Repeat("a", 64)}
	handleAndWait(s, cfg, owner, testInteraction("ask", "edit fixture"))
	other := testInteraction("approve", "approval-fixture")
	other.Member.User.ID = "888888888888888888"
	handleAndWait(s, cfg, owner, other)
	backend.mu.Lock()
	backend.pending.ArgumentsHash = strings.Repeat("b", 64)
	backend.mu.Unlock()
	handleAndWait(s, cfg, owner, testInteraction("approve", "approval-fixture"))
	if _, _, calls := backend.counts(); calls != 0 {
		t.Fatal("cross-user or changed approval executed")
	}
	backend.mu.Lock()
	backend.pending.ArgumentsHash = strings.Repeat("a", 64)
	backend.mu.Unlock()
	approvalEvent := testInteraction("approve", "approval-fixture")
	handleAndWait(s, cfg, owner, approvalEvent)
	handleAndWait(s, cfg, owner, approvalEvent)
	if _, _, calls := backend.counts(); calls != 1 {
		t.Fatalf("approval effects=%d", calls)
	}
	if backend.lastActor != "discord:"+testUser {
		t.Fatalf("actor=%s", backend.lastActor)
	}
}

func TestBridgeDoesNotExecuteAfterFailedAcknowledgementOrLargePreview(t *testing.T) {
	s, backend, rest, cfg, owner := testBridge(t)
	rest.ackStatus = 500
	event := testInteraction("ask", "not executed")
	handleAndWait(s, cfg, owner, event)
	rest.mu.Lock()
	rest.ackStatus = 0
	rest.mu.Unlock()
	handleAndWait(s, cfg, owner, event)
	if _, calls, _ := backend.counts(); calls != 0 {
		t.Fatal("unacknowledged/replayed action executed")
	}
	backend.pending = &ApprovalSummary{ID: "approval-large", ToolName: "workspace.write_file", Summary: "large file", Preview: strings.Repeat("x", 1201), ArgumentsHash: strings.Repeat("a", 64)}
	handleAndWait(s, cfg, owner, testInteraction("ask", "edit"))
	handleAndWait(s, cfg, owner, testInteraction("approve", "approval-large"))
	if _, _, calls := backend.counts(); calls != 0 {
		t.Fatal("truncated approval was executable")
	}
}

func TestBridgeConfigFailsClosedAndCancellation(t *testing.T) {
	s, backend, _, cfg, owner := testBridge(t)
	bad := cfg
	bad.UserIDs = []string{"*"}
	if _, err := s.Configure(context.Background(), bad); err == nil {
		t.Fatal("wildcard accepted")
	}
	bad = cfg
	bad.UserIDs = nil
	if err := validateConfig(bad, true); err == nil {
		t.Fatal("empty allowlist accepted")
	}
	foreign := identity.WithPrincipal(context.Background(), "unknown-owner")
	if _, err := s.Configure(foreign, cfg); err == nil {
		t.Fatal("foreign principal configured bridge")
	}
	backend.block = true
	s.handleInteraction(context.Background(), cfg, owner, testInteraction("ask", "wait"))
	deadline := time.Now().Add(time.Second)
	for {
		_, calls, _ := backend.counts()
		if calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ask never started")
		}
		time.Sleep(time.Millisecond)
	}
	handleAndWait(s, cfg, owner, testInteraction("cancel", ""))
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(context.Background())
	if err != nil || status.Config.Enabled || status.ActiveRequests != 0 {
		t.Fatalf("stop=%+v err=%v", status, err)
	}
}

func TestBridgeGatewayResumeReplayedInteractionAndHeartbeat(t *testing.T) {
	s, backend, rest, _, _ := testBridge(t)
	var connections, heartbeats atomic.Int32
	var firstOpMu sync.Mutex
	firstOps := []int{}
	event := testInteraction("ask", "one effect")
	upgrader := websocket.Upgrader{}
	var gatewayServer *httptest.Server
	gatewayServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		number := connections.Add(1)
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 60}})
		var auth gatewayPacket
		for {
			if conn.ReadJSON(&auth) != nil {
				return
			}
			if auth.Op != 1 {
				break
			}
			_ = conn.WriteJSON(map[string]any{"op": 11})
		}
		firstOpMu.Lock()
		firstOps = append(firstOps, auth.Op)
		firstOpMu.Unlock()
		var authData map[string]any
		_ = json.Unmarshal(auth.Data, &authData)
		if authData["token"] != testBotToken {
			t.Error("missing mock bot authentication")
		}
		if number == 1 && authData["intents"] != float64(0) {
			t.Error("bridge requested message content intents")
		}
		if number == 1 {
			_ = conn.WriteJSON(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{"session_id": "fixture-session", "resume_gateway_url": "ws" + strings.TrimPrefix(gatewayServer.URL, "http"), "application": map[string]string{"id": testApp}, "user": map[string]string{"id": "999999999999999999"}}})
		} else {
			_ = conn.WriteJSON(map[string]any{"op": 0, "s": 3, "t": "RESUMED", "d": map[string]any{}})
		}
		_ = conn.WriteJSON(map[string]any{"op": 0, "s": 2, "t": "INTERACTION_CREATE", "d": event})
		for {
			var packet gatewayPacket
			if conn.ReadJSON(&packet) != nil {
				return
			}
			if packet.Op == 1 {
				heartbeats.Add(1)
				_ = conn.WriteJSON(map[string]any{"op": 11})
				if number == 1 {
					_ = conn.WriteJSON(map[string]any{"op": 7})
					return
				}
			}
		}
	}))
	defer gatewayServer.Close()
	rest.mu.Lock()
	rest.gatewayURL = "ws" + strings.TrimPrefix(gatewayServer.URL, "http")
	rest.mu.Unlock()
	requestCtx, cancel := context.WithCancel(context.Background())
	if err := s.Start(requestCtx); err != nil {
		t.Fatal(err)
	}
	cancel() // the HTTP request ending must not stop the bridge.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if connections.Load() >= 2 && heartbeats.Load() >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if connections.Load() < 2 || heartbeats.Load() < 2 {
		t.Fatalf("resume/heartbeat connections=%d beats=%d", connections.Load(), heartbeats.Load())
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstOpMu.Lock()
	defer firstOpMu.Unlock()
	if len(firstOps) < 2 || firstOps[0] != 2 || firstOps[1] != 6 {
		t.Fatalf("Identify/Resume = %v", firstOps)
	}
	if _, calls, _ := backend.counts(); calls != 1 {
		t.Fatalf("gateway replay executed %d times", calls)
	}
}

func TestBridgeBoundedMessagesAndRESTFailureRedaction(t *testing.T) {
	s, _, rest, _, _ := testBridge(t)
	text := truncateMessage(strings.Repeat("😀", 100000), 1900)
	if textUnits(text) > 1900 || !strings.Contains(text, "complete response") {
		t.Fatalf("bad truncation units=%d", textUnits(text))
	}
	rest.globalRate = true
	if err := s.request(context.Background(), http.MethodGet, "/mock", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"ws://gateway.discord.gg", "wss://evil.example", "wss://gateway.discord.gg.evil.example", "wss://user:password@gateway.discord.gg"} {
		if _, err := safeGatewayURL(raw, false); err == nil {
			t.Fatalf("unsafe gateway %s", raw)
		}
	}
	if freshInteraction("111111111111111111", time.Now()) {
		t.Fatal("stale snowflake accepted")
	}
}

func TestBridgeConfigAndTokenChangesRevokePreviousBindings(t *testing.T) {
	s, backend, _, cfg, owner := testBridge(t)
	backend.pending = &ApprovalSummary{ID: "approval-revoke", ToolName: "workspace.write_file", Summary: "replace fixture", Preview: "const value = `exact`;", ArgumentsHash: strings.Repeat("a", 64)}
	text, allowed := formatApproval(*backend.pending)
	if !allowed || !strings.Contains(text, "`exact`") {
		t.Fatal("approval preview changed source backticks")
	}
	handleAndWait(s, cfg, owner, testInteraction("ask", "edit"))
	if _, err := s.Configure(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	handleAndWait(s, cfg, owner, testInteraction("approve", "approval-revoke"))
	if _, _, calls := backend.counts(); calls != 0 {
		t.Fatal("configuration save retained approval authority")
	}
	backend.mu.Lock()
	backend.pending = nil
	backend.mu.Unlock()
	handleAndWait(s, cfg, owner, testInteraction("ask", "new after config"))
	if newCalls, _, _ := backend.counts(); newCalls != 2 {
		t.Fatalf("configuration save retained session binding: %d", newCalls)
	}
	if _, err := s.SetToken(context.Background(), "rotated-fixture-token"); err != nil {
		t.Fatal(err)
	}
	handleAndWait(s, cfg, owner, testInteraction("ask", "new after token"))
	if newCalls, _, _ := backend.counts(); newCalls != 3 {
		t.Fatalf("token rotation retained session binding: %d", newCalls)
	}
	preview := ApprovalSummary{ID: "fenced", ArgumentsHash: strings.Repeat("a", 64), Preview: "```js\nunsafe fence\n```"}
	if _, allowed := formatApproval(preview); allowed {
		t.Fatal("ambiguous fenced preview allowed remote approval")
	}
}

func TestBridgeStatusRemainsAvailableWhileAskRuns(t *testing.T) {
	s, backend, rest, cfg, owner := testBridge(t)
	backend.block = true
	s.handleInteraction(context.Background(), cfg, owner, testInteraction("ask", "wait"))
	deadline := time.Now().Add(time.Second)
	for {
		_, calls, _ := backend.counts()
		if calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ask did not start")
		}
		time.Sleep(time.Millisecond)
	}
	s.handleInteraction(context.Background(), cfg, owner, testInteraction("status", ""))
	found := false
	for time.Now().Before(deadline) {
		rest.mu.Lock()
		for _, request := range rest.requests {
			body, _ := request["body"].(map[string]any)
			if request["method"] == http.MethodPatch && body["content"] == "Ready" {
				found = true
			}
		}
		rest.mu.Unlock()
		if found {
			break
		}
		time.Sleep(time.Millisecond)
	}
	handleAndWait(s, cfg, owner, testInteraction("cancel", ""))
	if !found {
		t.Fatal("status was blocked behind running ask")
	}
}

func TestBridgeRetentionAndInvalidConfigErrors(t *testing.T) {
	s, _, _, cfg, owner := testBridge(t)
	ctx := context.Background()
	for _, item := range []struct {
		id  string
		age time.Duration
	}{{"old-receipt", 25 * time.Hour}, {"recent-receipt", time.Hour}} {
		if _, err := s.store.DB.Exec(`INSERT INTO discord_bridge_interactions VALUES(?,'status','scope','completed',?)`, item.id, time.Now().Add(-item.age).UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	handleAndWait(s, cfg, owner, testInteraction("status", ""))
	var count int
	if err := s.store.DB.QueryRow(`SELECT COUNT(*) FROM discord_bridge_interactions WHERE interaction_id='old-receipt'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old receipt retained %d %v", count, err)
	}
	if err := s.store.DB.QueryRow(`SELECT COUNT(*) FROM discord_bridge_interactions WHERE interaction_id='recent-receipt'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("recent replay protection was removed")
	}
	cfg.UserIDs = []string{"*"}
	if _, err := s.Configure(ctx, cfg); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("validation error is not typed: %v", err)
	}
	if _, err := s.Status(identity.WithPrincipal(ctx, "foreign")); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign principal error is not typed: %v", err)
	}
}

func TestBridgeRESTRedirectOversizeAndCredentialURLsDoNotLeak(t *testing.T) {
	s, _, _, _, _ := testBridge(t)
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1); fmt.Fprint(w, `{}`) }))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	s.apiBase = redirect.URL
	if err := s.request(context.Background(), http.MethodGet, "/redirect", nil, true, nil); err == nil {
		t.Fatal("redirect accepted")
	}
	if destinationCalls.Load() != 0 {
		t.Fatal("bot token request followed redirect")
	}
	redirect.Close()
	if err := s.request(context.Background(), http.MethodPatch, "/webhooks/app/secret-interaction-token/messages/@original", map[string]any{}, false, nil); err == nil || strings.Contains(err.Error(), "secret-interaction-token") || strings.Contains(err.Error(), redirect.URL) {
		t.Fatalf("transport error leaked secret URL: %v", err)
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", (1<<20)+1)) }))
	defer large.Close()
	s.apiBase = large.URL
	if err := s.request(context.Background(), http.MethodGet, "/oversized", nil, false, nil); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized response accepted: %v", err)
	}
}

func TestBridgeStopCancelsGatewayWaitingForHello(t *testing.T) {
	s, _, rest, _, _ := testBridge(t)
	connected := make(chan struct{})
	upgrader := websocket.Upgrader{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		close(connected)
		var packet gatewayPacket
		_ = conn.ReadJSON(&packet)
	}))
	defer gateway.Close()
	rest.mu.Lock()
	rest.gatewayURL = "ws" + strings.TrimPrefix(gateway.URL, "http")
	rest.mu.Unlock()
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway never connected")
	}
	start := time.Now()
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("Stop waited for Hello instead of canceling the connection")
	}
}
