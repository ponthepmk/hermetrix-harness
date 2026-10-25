package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

type outputScopeAdapter struct {
	base           *goalAdapter
	capturedTask   *worker.Task
	proposalCalls  int
	selectionCalls int
	selectOverride []string
	proposePaths   []string
	proposeContent map[string]string
}

func (a *outputScopeAdapter) StreamChat(ctx context.Context, p providers.Profile, url string, r providers.ChatRequest, delta func(providers.Delta) error) (providers.Completion, error) {
	name := r.Tools[0].Function.Name
	switch name {
	case "submit_file_selection":
		a.selectionCalls++
		if a.selectOverride != nil {
			body, _ := json.Marshal(map[string]any{"files": a.selectOverride, "rationale": "scoped"})
			return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
		}
	case "submit_changes":
		a.proposalCalls++
		var task worker.Task
		if err := json.Unmarshal([]byte(r.Messages[1].Content), &task); err != nil {
			return providers.Completion{}, err
		}
		a.capturedTask = &task
		if a.proposePaths != nil {
			changes := make([]any, 0, len(a.proposePaths))
			for _, path := range a.proposePaths {
				content := a.proposeContent[path]
				if content == "" {
					content = task.Files[path]
				}
				changes = append(changes, map[string]any{"path": path, "content": content})
			}
			body, _ := json.Marshal(map[string]any{"summary": "scoped", "recommended_checks": []string{"node test.js"}, "changes": changes})
			return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: name, Arguments: string(body)}}}, nil
		}
	}
	return a.base.StreamChat(ctx, p, url, r, delta)
}

func setupOutputScope(t *testing.T, contract taskengine.GoalContract, outputs []string) (*Service, *outputScopeAdapter, taskengine.RunAuthority, string, taskengine.StepPacket, string, string) {
	t.Helper()
	ctx := context.Background()
	base := &goalAdapter{}
	adapter := &outputScopeAdapter{base: base}
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, contract)
	c.providers = providers.NewService(db, adapter)
	task, err := ts.CreatePlan(ctx, taskengine.CreatePlanInput{
		TaskID: task.ID, ExpectedTaskRevision: task.Revision,
		RequirementRevision: task.ActiveRequirementRevision, Reason: "output scope", Actor: "planner",
		Steps: []taskengine.StepSpec{{
			Key:            "fix",
			Title:          "Correct sum",
			Instructions:   "Fix sum.js so node test.js passes",
			RequirementIDs: []string{"AC-1"},
			Checks:         []string{"node test.js"},
			EffectScope:    []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"},
			OutputPaths:    outputs,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := ts.BuildNextStepPacket(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := ts.BeginRun(ctx, taskengine.BeginRunInput{TaskID: task.ID, ExpectedTaskRevision: task.Revision, Owner: "coordinator", LeaseDuration: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	task, err = ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := ts.BeginStepAttempt(ctx, taskengine.BeginAttemptInput{
		RunID: run.ID, LeaseToken: run.LeaseToken, StepKey: "fix",
		ExpectedTaskRevision: task.Revision, ExpectedStepRevision: task.Plan.Steps[0].Revision,
		InputHash: packet.CanonicalPacketHash, Packet: &packet,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, adapter, taskengine.RunAuthority{RunID: run.ID, LeaseToken: run.LeaseToken}, attempt.ID, packet, input.ProviderID, root
}

func TestStepOutputScopeExecution(t *testing.T) {
	t.Run("SelectionRetainsOmittedRequired", func(t *testing.T) {
		c, adapter, authority, attemptID, _, providerID, _ := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js", "later.js"}}, []string{"sum.js"})
		ctx := context.Background()
		adapter.selectOverride = []string{"test.js"}
		out, err := c.SelectFiles(ctx, SelectFilesInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Result.Files) != 2 || out.Result.Files[0] != "sum.js" || out.Result.Files[1] != "test.js" {
			t.Fatalf("selection=%v want [sum.js test.js]", out.Result.Files)
		}
		firstArtifact := out.Artifact.ID
		calls := adapter.selectionCalls
		out2, err := c.SelectFiles(ctx, SelectFilesInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority})
		if err != nil {
			t.Fatal(err)
		}
		if out2.Artifact.ID != firstArtifact {
			t.Fatalf("replay artifact=%s want %s", out2.Artifact.ID, firstArtifact)
		}
		if adapter.selectionCalls != calls {
			t.Fatalf("replay made extra provider calls %d→%d", calls, adapter.selectionCalls)
		}
	})

	t.Run("DirectProposalRetainsOmittedRequired", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, _ := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js", "later.js"}}, []string{"sum.js"})
		ctx := context.Background()
		_, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet, Files: []string{"test.js"}})
		if err != nil {
			t.Fatal(err)
		}
		if adapter.capturedTask == nil {
			t.Fatal("no captured task")
		}
		if _, ok := adapter.capturedTask.Files["sum.js"]; !ok {
			t.Fatal("captured Files missing required output sum.js")
		}
		if _, ok := adapter.capturedTask.Files["test.js"]; !ok {
			t.Fatal("captured Files missing selected test.js")
		}
		if len(adapter.capturedTask.WritablePaths) != 1 || adapter.capturedTask.WritablePaths[0] != "sum.js" {
			t.Fatalf("captured WritablePaths=%v want [sum.js]", adapter.capturedTask.WritablePaths)
		}
	})

	t.Run("EmptySelectionForExplicitOutputs", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, _ := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js"}}, []string{"sum.js"})
		ctx := context.Background()
		_, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet})
		if err != nil {
			t.Fatalf("explicit outputs with nil files should succeed: %v", err)
		}
		if adapter.capturedTask == nil {
			t.Fatal("no captured task")
		}
		if _, ok := adapter.capturedTask.Files["sum.js"]; !ok {
			t.Fatal("captured Files missing auto-included sum.js")
		}

		c2, adapter2, authority2, attemptID2, packet2, providerID2, _ := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js"}}, nil)
		_, err = c2.Propose(ctx, ProposalInput{AttemptID: attemptID2, ProviderID: providerID2, Authority: authority2, Packet: packet2})
		if err == nil {
			t.Fatal("legacy blank outputs with nil files must reject")
		}
		if adapter2.proposalCalls != 0 {
			t.Fatalf("legacy reject made %d proposal calls, want 0", adapter2.proposalCalls)
		}
	})

	t.Run("FutureStepContextReadonly", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, root := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js", "test.js"}}, []string{"sum.js"})
		ctx := context.Background()
		adapter.proposePaths = []string{"test.js"}
		adapter.proposeContent = map[string]string{"test.js": "const x=1;\n"}
		out, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet, Files: []string{"sum.js", "test.js"}})
		if err == nil {
			t.Fatal("out-of-scope change to test.js must be rejected")
		}
		if out.Proposal.ID != "" {
			t.Fatalf("rejected proposal must not register an ID, got %q", out.Proposal.ID)
		}
		if adapter.capturedTask == nil {
			t.Fatal("no captured task")
		}
		if len(adapter.capturedTask.WritablePaths) != 1 || adapter.capturedTask.WritablePaths[0] != "sum.js" {
			t.Fatalf("captured WritablePaths=%v want [sum.js]", adapter.capturedTask.WritablePaths)
		}
		data, err := os.ReadFile(filepath.Join(root, "test.js"))
		if err != nil {
			t.Fatal(err)
		}
		want := "const assert=require('node:assert/strict');assert.equal(require('./sum')(2,3),5);assert.equal(require('./sum')(-4,7),3);console.log('acceptance passed');\n"
		if string(data) != want {
			t.Fatal("test.js was modified")
		}
	})

	t.Run("AbsentRequiredMustBeCreated", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, root := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js", "new.js"}}, []string{"sum.js", "new.js"})
		ctx := context.Background()
		out, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet, Files: []string{"sum.js", "test.js"}})
		if err == nil {
			t.Fatal("required absent output new.js not created; proposal must be rejected")
		}
		if out.Proposal.ID != "" {
			t.Fatalf("rejected proposal must not register an ID, got %q", out.Proposal.ID)
		}
		if adapter.capturedTask == nil {
			t.Fatal("no captured task")
		}
		if content, ok := adapter.capturedTask.Files["new.js"]; !ok || content != "" {
			t.Fatalf("captured Files new.js: ok=%v content=%q want present and empty", ok, content)
		}
		found := false
		for _, f := range adapter.capturedTask.AbsentFiles {
			if f == "new.js" {
				found = true
			}
		}
		if !found {
			t.Fatal("captured AbsentFiles missing new.js")
		}
		if _, err := os.Stat(filepath.Join(root, "new.js")); !os.IsNotExist(err) {
			t.Fatal("new.js must remain absent on disk")
		}
	})

	t.Run("ExistingRequiredMayStayUnchanged", func(t *testing.T) {
		c, adapter, authority, attemptID, packet, providerID, _ := setupOutputScope(t,
			taskengine.GoalContract{WritablePaths: []string{"sum.js", "test.js"}}, []string{"sum.js", "test.js"})
		ctx := context.Background()
		_, err := c.Propose(ctx, ProposalInput{AttemptID: attemptID, ProviderID: providerID, Authority: authority, Packet: packet, Files: []string{"sum.js", "test.js"}})
		if err != nil {
			t.Fatalf("existing required outputs may stay unchanged: %v", err)
		}
		if adapter.capturedTask == nil {
			t.Fatal("no captured task")
		}
		if len(adapter.capturedTask.WritablePaths) != 2 {
			t.Fatalf("captured WritablePaths=%v want 2 entries", adapter.capturedTask.WritablePaths)
		}
	})
}
