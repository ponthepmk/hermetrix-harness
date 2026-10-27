package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hermetrix-harness/internal/agentruntime"
	"hermetrix-harness/internal/providers"
	toolruntime "hermetrix-harness/internal/tools"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDesktopApprovedViewSendsOwnedPixelsOnRealProviderWire(t *testing.T) {
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	calls := 0
	args := `{"action":"view","handle":1,"process_id":2,"observation_hash":"` + strings.Repeat("a", 64) + `"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var wire map[string]any
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"view-call\",\"type\":\"function\",\"function\":{\"name\":\"desktop\",\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", args)
		} else {
			body, _ := json.Marshal(wire)
			if !strings.Contains(string(body), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pixels.Bytes())) {
				t.Error("approved image bytes missing on provider wire")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"received screen fixture\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	root := t.TempDir()
	service, provider, cleanup := testAgentServiceAtRoot(t, server, root)
	defer cleanup()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := service.store.DB.ExecContext(ctx, `INSERT INTO runtime_fingerprints(id,digest,runtime_kind,endpoint_identity,model_id,config_digest,evidence_quality,created_at) VALUES('vision-fixture','vision-fixture','fixture','loopback','fixture','fixture','fixture',?)`, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.store.DB.ExecContext(ctx, `UPDATE provider_profiles SET runtime_fingerprint_id='vision-fixture' WHERE id=?`, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	provider, err = service.providers.Get(ctx, provider.ID)
	if err != nil {
		t.Fatal(err)
	}
	qualification := insertQualification(t, service, provider, "certified-64k", "")
	_, err = service.store.DB.ExecContext(ctx, `UPDATE model_qualification_runs SET runtime_fingerprint_id='vision-fixture',modalities_json='["text","image"]' WHERE id=?`, qualification)
	if err != nil {
		t.Fatal(err)
	}
	project := createTestProject(t, service, root)
	ref, err := service.store.Blobs.Put(pixels.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := service.store.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.store.DB.ExecContext(ctx, `INSERT INTO artifacts(id,project_id,name,kind,mime_type,blob_ref,byte_size,checksum,metadata_json,created_at,owner_principal_id) VALUES('screen-fixture',?,'screen.png','source_image','image/png',?,?,?,'{}',?,?)`, project, ref, pixels.Len(), ref, now, owner)
	if err != nil {
		t.Fatal(err)
	}
	service.WithDesktop(&fixtureDesktop{image: "screen-fixture"})
	session, err := service.CreateSession(ctx, CreateSessionInput{ProviderID: provider.ID, ProjectID: project, ContextProfile: "certified-64k"})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.RunTurn(ctx, session.ID, TurnInput{Content: "inspect this fixture screen"}, nil)
	if err != nil || paused.Approval == nil || calls != 1 {
		t.Fatalf("view did not wait for sharing approval: %+v %v", paused, err)
	}
	completed, err := service.DecideApproval(ctx, paused.Approval.ID, ApprovalDecisionInput{Actor: "fixture user", Decision: "approve"}, nil)
	if err != nil || calls != 2 || completed.AssistantEvent.Content != "received screen fixture" {
		t.Fatalf("view continuation failed: %+v calls=%d err=%v", completed, calls, err)
	}
	if err := service.store.DB.QueryRowContext(ctx, `SELECT token_sample FROM provider_profiles WHERE id=?`, provider.ID).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("image tokens contaminated text calibration")
	}
}

type fixtureDesktop struct {
	prepared, confirmed, cancelled int
	last                           agentruntime.DesktopActionInput
	image                          string
	state                          string
}

func (f *fixtureDesktop) ListDesktopWindows(context.Context) ([]agentruntime.DesktopWindow, error) {
	return []agentruntime.DesktopWindow{{Handle: 1, ProcessID: 2, Title: "Fixture"}}, nil
}
func (f *fixtureDesktop) ObserveDesktopWindow(context.Context, int64, int) (agentruntime.DesktopObservation, error) {
	return agentruntime.DesktopObservation{Window: agentruntime.DesktopWindow{Handle: 1, ProcessID: 2}, ObservationHash: strings.Repeat("a", 64), ScreenshotArtifactID: f.image}, nil
}
func (f *fixtureDesktop) PrepareDesktopAction(_ context.Context, in agentruntime.DesktopActionInput) (agentruntime.DesktopAction, error) {
	f.prepared++
	f.last = in
	return agentruntime.DesktopAction{ID: "native-one", SubjectHash: "frozen-native", ScreenshotBeforeID: "before", Target: agentruntime.DesktopElement{Name: "Fixture button"}}, nil
}
func (f *fixtureDesktop) ConfirmDesktopAction(_ context.Context, id, hash, text, actor string) (agentruntime.DesktopAction, error) {
	if id != "native-one" || hash != "frozen-native" {
		return agentruntime.DesktopAction{}, fmt.Errorf("changed native receipt")
	}
	f.confirmed++
	state := f.state
	if state == "" {
		state = "completed"
	}
	return agentruntime.DesktopAction{ID: id, State: state, ScreenshotBeforeID: "before", ScreenshotAfterID: "after"}, nil
}
func (f *fixtureDesktop) CancelDesktopAction(_ context.Context, id, hash string) (agentruntime.DesktopAction, error) {
	f.cancelled++
	return agentruntime.DesktopAction{ID: id, State: "cancelled"}, nil
}

func TestDesktopChatApprovalFreezesNativeReceiptAndCannotReplay(t *testing.T) {
	for _, decision := range []string{"approve", "deny"} {
		t.Run(decision, func(t *testing.T) {
			calls := 0
			args := `{"action":"click","handle":1,"process_id":2,"point":{"x":10,"y":20},"observation_hash":"` + strings.Repeat("a", 64) + `"}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				if calls == 1 {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"native-call\",\"type\":\"function\",\"function\":{\"name\":\"desktop\",\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", args)
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"reviewed result\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer server.Close()
			root := t.TempDir()
			service, provider, cleanup := testAgentServiceAtRoot(t, server, root)
			defer cleanup()
			project := createTestProject(t, service, root)
			native := &fixtureDesktop{}
			service.WithDesktop(native)
			session, err := service.CreateSession(context.Background(), CreateSessionInput{ProviderID: provider.ID, ProjectID: project, ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
			if err != nil {
				t.Fatal(err)
			}
			paused, err := service.RunTurn(context.Background(), session.ID, TurnInput{Content: "click fixture once"}, nil)
			if err != nil || paused.Approval == nil || native.prepared != 1 || native.confirmed != 0 {
				t.Fatalf("did not stop before input: %+v native=%+v err=%v", paused, native, err)
			}
			if paused.Approval.Metadata["desktop_action_id"] != "native-one" || paused.Approval.Metadata["screenshot_artifact_id"] != "before" {
				t.Fatal("approval lost native receipt or image")
			}
			if _, err = service.DecideApproval(context.Background(), paused.Approval.ID, ApprovalDecisionInput{Actor: "fixture user", Decision: decision}, nil); err != nil {
				t.Fatal(err)
			}
			if native.prepared != 1 || (decision == "approve" && native.confirmed != 1) || (decision == "deny" && (native.confirmed != 0 || native.cancelled != 1)) {
				t.Fatalf("approval re-prepared, leaked or replayed: %+v", native)
			}
			if _, err = service.DecideApproval(context.Background(), paused.Approval.ID, ApprovalDecisionInput{Actor: "fixture user", Decision: "approve"}, nil); err == nil {
				t.Fatal("decision replay allowed")
			}
		})
	}
}
func TestDesktopRejectsUnapprovedInputAndTextOnlyVision(t *testing.T) {
	service, provider, cleanup := testAgentService(t, successProviderServer(t))
	defer cleanup()
	session := Session{ProviderID: provider.ID}
	native := &fixtureDesktop{}
	service.WithDesktop(native)
	call := providers.ToolCall{Name: "desktop", Arguments: `{"action":"click","handle":1,"process_id":2,"point":{"x":0,"y":0}}`}
	receipt := service.executeDesktopTool(context.Background(), call, toolruntime.Definition{Name: "desktop"}, nil)
	if receipt.Status != "failed" || native.confirmed != 0 {
		t.Fatal("unapproved mouse input dispatched")
	}
	call.Arguments = `{"action":"view","handle":1,"process_id":2,"observation_hash":"a"}`
	if _, err := service.enrichDesktopApproval(context.Background(), session, call, toolruntime.ApprovalPlan{}); err == nil || !strings.Contains(err.Error(), "image capability") {
		t.Fatalf("text-only model accepted pixels: %v", err)
	}
	for _, raw := range []string{`{"action":"click","handle":1,"process_id":2,"ref":1}`, `{"action":"click","handle":1,"process_id":2,"point":{"x":1,"y":2},"text":"hidden"}`, `{"action":"set_value","handle":1,"process_id":2,"ref":1,"unknown":true}`} {
		if _, err := decodeDesktopArgs(raw); err == nil {
			t.Fatalf("invalid args accepted: %s", raw)
		}
	}
	// Observation receipts cannot make the model receive a bitmap implicitly.
	body, _ := json.Marshal(toolruntime.Receipt{Name: "desktop", Status: "succeeded", Metadata: map[string]any{"screenshot_artifact_id": "private"}})
	part, _, err := service.desktopImageForTurn(context.Background(), []Event{{EventKind: "tool_result", TurnID: "turn", Content: string(body), Metadata: map[string]any{"tool_name": "desktop"}}}, "turn", providers.Profile{})
	if err != nil || part != nil {
		t.Fatal("observation shared an unapproved image")
	}
	// A failed or uncertain newer action invalidates older view pixels; the
	// model must not reason about the previous frame as the current screen.
	oldView, _ := json.Marshal(toolruntime.Receipt{Name: "desktop", Status: "succeeded", Metadata: map[string]any{"vision_image_artifact_id": "old-private-frame", "desktop_image_provider_id": "old-provider"}})
	for _, latest := range []string{`{"status":"failed"}`, `{"status":"uncertain"}`, `invalid-json`} {
		events := []Event{
			{EventKind: "tool_result", TurnID: "turn", Content: string(oldView), Metadata: map[string]any{"tool_name": "desktop"}},
			{EventKind: "tool_result", TurnID: "turn", Content: latest, Metadata: map[string]any{"tool_name": "desktop"}},
		}
		part, _, err := service.desktopImageForTurn(context.Background(), events, "turn", providers.Profile{})
		if err != nil || part != nil {
			t.Fatalf("newer failed receipt reused previous pixels: %s %v", latest, err)
		}
	}
}
