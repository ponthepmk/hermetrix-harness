package taskcoord

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
)

func TestGoalCreatesNewSourceRollsBackToAbsenceAndReplans(t *testing.T) {
	a := &goalAdapter{failures: 2}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	if err := os.Remove(filepath.Join(root, "sum.js")); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || !a.sawAbsentPlan || !a.sawAbsentSelection || !a.sawAbsentProposal || a.proposals != 3 || a.plans != 2 {
		t.Fatalf("goal=%+v adapter=%+v", g, a)
	}
	body, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil || !strings.Contains(string(body), "a+b") {
		t.Fatalf("created code=%s err=%v", body, err)
	}
	actions, err := ts.GoalActions(context.Background(), g.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	proofs := 0
	for _, action := range actions {
		if action.Receipt["rolled_back"] != true {
			continue
		}
		id, _ := action.Receipt["rollback_artifact_id"].(string)
		_, body, loadErr := c.products.GetArtifact(context.Background(), id)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		var receipt rollbackReceipt
		if err = json.Unmarshal(body, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Restored && len(receipt.Files) == 1 && receipt.Files[0].OriginalSHA256 == "absent" && receipt.Files[0].AfterSHA256 == "absent" && receipt.Files[0].RestorationArtifactID != "" && receipt.Files[0].RestorationOperationID != "" {
			proofs++
		}
	}
	if proofs != 2 {
		t.Fatalf("missing absence rollback proofs=%d", proofs)
	}
}

func TestGoalStopsOnProjectRootChangeBeforeAnyProviderOrWrite(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, input, _ := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err = c.products.SaveProject(context.Background(), product.ProjectInput{ID: task.ProjectID, Name: "moved project", RootPath: other}); err != nil {
		t.Fatal(err)
	}
	g, err = c.AdvanceGoal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalWaiting || a.plans != 0 || g.ActionsUsed != 0 {
		t.Fatalf("root redirect dispatched work: goal=%+v adapter=%+v err=%v", g, a, err)
	}
	if _, err = os.Stat(filepath.Join(other, "sum.js")); !os.IsNotExist(err) {
		t.Fatal("goal wrote redirected project")
	}
	_, _ = ts.StopGoal(context.Background(), g.ID)
}

func TestApplyCreationRejectsRootChangedAfterProposalSnapshot(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	if err := os.Remove(filepath.Join(root, "sum.js")); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	x, err := ts.Execution(context.Background(), g.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ts.DecideCodeProposal(context.Background(), x.Proposal.ID, "owner", taskengine.ProposalApproved, "reviewed exact root", nil); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if _, err = c.products.SaveProject(context.Background(), product.ProjectInput{ID: task.ProjectID, Name: "moved", RootPath: other}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Apply(context.Background(), taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}, x.Proposal.ID, "owner"); err == nil {
		t.Fatal("proposal escaped its frozen root")
	}
	for _, dir := range []string{root, other} {
		if _, err = os.Stat(filepath.Join(dir, "sum.js")); !os.IsNotExist(err) {
			t.Fatalf("unreviewed root write in %s", dir)
		}
	}
}

func TestGoalCreateProposalRefusesFileCreatedAfterSnapshot(t *testing.T) {
	a := &goalAdapter{}
	c, ts, _, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	if err := os.Remove(filepath.Join(root, "sum.js")); err != nil {
		t.Fatal(err)
	}
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "sum.js"), []byte("owner created this meanwhile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.AdvanceGoal(context.Background(), g.ID); err == nil {
		t.Fatal("stale creation proposal overwrote a new file")
	}
	g, err = ts.Goal(context.Background(), g.ID)
	if err != nil || g.State != taskengine.GoalWaiting {
		t.Fatalf("goal=%+v err=%v", g, err)
	}
	body, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if string(body) != "owner created this meanwhile\n" {
		t.Fatalf("owner file changed: %s", body)
	}
}

func TestVerificationRefusesRootChangedAfterApply(t *testing.T) {
	a := &goalAdapter{}
	c, ts, task, input, root := newGoalFixture(t, a, taskengine.GoalContract{})
	g, err := c.StartGoal(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err = c.AdvanceGoal(context.Background(), g.ID); err != nil {
			t.Fatal(err)
		}
	}
	x, err := ts.Execution(context.Background(), task.ID)
	if err != nil || x.Proposal.State != taskengine.ProposalApplied {
		t.Fatalf("applied execution=%+v err=%v", x, err)
	}
	applied, err := os.ReadFile(filepath.Join(root, "sum.js"))
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	marker := "require('fs').writeFileSync('command-ran','wrong root');\n"
	if err = os.WriteFile(filepath.Join(other, "test.js"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = c.products.SaveProject(context.Background(), product.ProjectInput{ID: task.ProjectID, Name: "moved", RootPath: other}); err != nil {
		t.Fatal(err)
	}
	output, err := c.VerifyFrozen(context.Background(), taskengine.RunAuthority{RunID: x.Run.ID, LeaseToken: x.Run.LeaseToken}, x.Proposal.ID, "owner")
	if err == nil || !strings.Contains(err.Error(), "project root changed") || len(output.Jobs) != 0 || output.RolledBack {
		t.Fatalf("redirected verification=%+v err=%v", output, err)
	}
	if _, err = os.Stat(filepath.Join(other, "command-ran")); !os.IsNotExist(err) {
		t.Fatal("verification launched in the new root")
	}
	data, readErr := os.ReadFile(filepath.Join(root, "sum.js"))
	if readErr != nil || string(data) != string(applied) {
		t.Fatalf("unproved rollback changed original root: %s err=%v", data, readErr)
	}
}
