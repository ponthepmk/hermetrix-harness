package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestGoalCannotCompleteAfterPassingCommandMutatesReadonlyContext(t *testing.T) {
	ctx := context.Background()
	a := &goalAdapter{}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	p := filepath.Join(root, "test.js")
	original, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	mutator := string(original) + "require('node:fs').appendFileSync(__filename,'\\n// readonly drift\\n');\n"
	if err = os.WriteFile(p, []byte(mutator), 0600); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(120 * time.Second)
	for g.State == taskengine.GoalActive && time.Now().Before(deadline) {
		_, _ = c.AdvanceGoal(ctx, g.ID)
		g, err = ts.Goal(ctx, g.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if g.State != taskengine.GoalWaiting || a.reviews != 0 {
		t.Fatalf("passing command changed readonly context but goal=%s reviews=%d", g.State, a.reviews)
	}
	changed, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(changed), "// readonly drift\n") {
		t.Fatal("mutation did not execute or harness overwrote readonly source", err)
	}
	x, err := ts.Execution(ctx, input.TaskID)
	if err != nil || x.Proposal == nil || x.Proposal.VerificationArtifactID != "" {
		t.Fatal("readonly drift acquired successful verification evidence", err)
	}
	commands := 0
	for _, effect := range x.Effects {
		if effect.Action == "workspace.run" {
			commands++
			if effect.State != taskengine.EffectObserved {
				t.Fatal("completed command effect was not observed")
			}
		}
	}
	if commands != 1 {
		t.Fatal("mutation check replayed", commands)
	}
}

func TestReductionCheckCoverageIsVersionedAndCannotDropBehavior(t *testing.T) {
	task := worker.Task{Files: map[string]string{"sum.js": "old", "test.js": "test"}, WritablePaths: []string{"sum.js"}}
	readonly := "assert-file-sha256 test.js " + worker.Hash("test")
	for _, tc := range []struct {
		name          string
		before, after []string
		version       int
		want          bool
	}{
		{"readonly_covered", []string{"node test.js", readonly}, []string{"node test.js"}, 2, true},
		{"legacy_stays_strict", []string{"node test.js", readonly}, []string{"node test.js"}, 1, false},
		{"wrong_hash", []string{"node test.js", "assert-file-sha256 test.js " + worker.Hash("other")}, []string{"node test.js"}, 2, false},
		{"unknown_path", []string{"node test.js", "assert-file-sha256 unknown.js " + worker.Hash("test")}, []string{"node test.js"}, 2, false},
		{"writable_hash", []string{"node test.js", "assert-file-sha256 sum.js " + worker.Hash("old")}, []string{"node test.js"}, 2, false},
		{"contains_assertion", []string{"node test.js", "assert-file-contains test.js test"}, []string{"node test.js"}, 2, false},
		{"behavior_changed", []string{"node test.js", readonly}, []string{"node other.js"}, 2, false},
		{"behavior_order_changed", []string{"node a.js", "node b.js", readonly}, []string{"node b.js", "node a.js"}, 2, false},
		{"no_behavior_remains", []string{readonly}, nil, 2, false},
		{"unsupported_recipe", []string{"node test.js"}, []string{"node test.js"}, 99, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reductionChecksCompatible(tc.before, tc.after, task, task.WritablePaths, tc.version); got != tc.want {
				t.Fatal("incorrect check coverage", got)
			}
		})
	}
}

func TestVerificationScopeCannotOmitContextOrDowngradePinnedPolicy(t *testing.T) {
	loaded := loadedProposal{Artifact: product.Artifact{Metadata: map[string]any{"project_root": "root"}}, InputTask: &worker.Task{Files: map[string]string{"sum.js": "old", "test.js": "test"}}, Bundle: proposalBundle{VerificationScopeVersion: 1, Result: worker.Result{Changes: []worker.Change{{Path: "sum.js", Content: "new"}}}}}
	snapshot := product.ProjectSnapshot{Root: "root", Hashes: map[string]string{"sum.js": worker.Hash("new"), "test.js": worker.Hash("test")}}
	if err := validateVerificationScope(loaded, snapshot, 1); err != nil {
		t.Fatal(err)
	}
	if err := validateVerificationScope(loaded, snapshot, 0); err == nil {
		t.Fatal("pinned scope downgraded")
	}
	delete(snapshot.Hashes, "test.js")
	if err := validateVerificationScope(loaded, snapshot, 1); err == nil {
		t.Fatal("readonly context removed from snapshot")
	}
	snapshot.Hashes["test.js"] = worker.Hash("other")
	if err := validateVerificationScope(loaded, snapshot, 1); err == nil {
		t.Fatal("wrong readonly preimage accepted")
	}
	snapshot.Hashes["test.js"] = worker.Hash("test")
	loaded.Bundle.VerificationScopeVersion = 0
	if err := validateVerificationScope(loaded, snapshot, 0); err != nil {
		t.Fatal("legacy scope changed", err)
	}
	loaded.Bundle.Reduction = &repairReductionProof{Version: 2}
	if err := validateVerificationScope(loaded, snapshot, 0); err == nil {
		t.Fatal("v2 derivation bypassed full scope")
	}
	if err := validateVerificationScope(loaded, snapshot, 99); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestVerifiedSnapshotRetainsReadonlyContextThroughReview(t *testing.T) {
	ctx := context.Background()
	a := &goalAdapter{}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	original, err := os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(120 * time.Second)
	var x taskengine.ExecutionSnapshot
	for g.State == taskengine.GoalActive && time.Now().Before(deadline) {
		_, _ = c.AdvanceGoal(ctx, g.ID)
		g, err = ts.Goal(ctx, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		x, err = ts.Execution(ctx, input.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if x.Proposal != nil && x.Proposal.VerificationArtifactID != "" {
			break
		}
	}
	if x.Proposal == nil || x.Proposal.VerificationArtifactID == "" {
		t.Fatal("verification never completed", g.State)
	}
	_, body, err := c.products.GetArtifact(ctx, x.Proposal.VerificationArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	var bundle verificationBundle
	if err = json.Unmarshal(body, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Snapshot.Hashes["test.js"] != worker.Hash(string(original)) {
		t.Fatal("successful snapshot omitted original readonly input")
	}
	if err = os.WriteFile(filepath.Join(root, "test.js"), append(original, []byte("// changed after checks\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = c.AdvanceGoal(ctx, g.ID)
	g, err = ts.Goal(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.State != taskengine.GoalWaiting || a.reviews != 0 {
		t.Fatal("readonly drift reached provider review or completion", g.State, a.reviews)
	}
}
