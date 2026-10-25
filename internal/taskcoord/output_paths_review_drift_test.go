package taskcoord

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

type explicitOutputReviewAdapter struct {
	held *heldReviewAdapter
}

func (a *explicitOutputReviewAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	completion, err := a.held.StreamChat(ctx, p, key, r, emit)
	if err != nil {
		return completion, err
	}
	if len(r.Tools) == 0 || r.Tools[0].Function.Name != "submit_plan" || len(completion.ToolCalls) == 0 {
		return completion, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(completion.ToolCalls[0].Arguments), &payload); err != nil {
		return completion, err
	}
	steps, ok := payload["steps"].([]any)
	if !ok || len(steps) == 0 {
		return completion, fmt.Errorf("unexpected plan shape")
	}
	step, ok := steps[0].(map[string]any)
	if !ok {
		return completion, fmt.Errorf("unexpected step shape")
	}
	step["output_paths"] = []string{"sum.js"}
	body, err := json.Marshal(payload)
	if err != nil {
		return completion, err
	}
	completion.ToolCalls[0].Arguments = string(body)
	return completion, nil
}

func TestExplicitOutputStillGuardsGlobalOtherOutputDuringReview(t *testing.T) {
	ctx := context.Background()
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, &goalAdapter{}, taskengine.GoalContract{WritablePaths: []string{"sum.js", "previous.js"}})
	if err := os.WriteFile(filepath.Join(root, "previous.js"), []byte("// earlier output\n"), 0600); err != nil {
		t.Fatal(err)
	}
	held := &heldReviewAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	wrapper := &explicitOutputReviewAdapter{held: held}
	c.providers = providers.NewService(db, wrapper)

	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		x, err := ts.Execution(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if x.Proposal != nil && x.Proposal.State == taskengine.ProposalAwaitingReview {
			break
		}
		if _, err = c.AdvanceGoal(ctx, g.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	x, err := ts.Execution(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Proposal == nil || x.Proposal.State != taskengine.ProposalAwaitingReview {
		t.Fatal("real check did not become ready for review")
	}

	reviewCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.AdvanceGoal(reviewCtx, g.ID); done <- err }()
	select {
	case <-held.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("review provider not entered")
	}
	ownerBytes := "// owner changed previous.js after verification\n"
	if err := os.WriteFile(filepath.Join(root, "previous.js"), []byte(ownerBytes), 0600); err != nil {
		t.Fatal(err)
	}
	close(held.release)
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("stale reviewer response did not report an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review did not settle")
	}

	current, err := ts.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	g, err = ts.Goal(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State == taskengine.StateCompleted || g.State == taskengine.GoalCompleted {
		t.Fatalf("STALE REVIEW ACCEPTED: task=%s goal=%s", current.State, g.State)
	}
	if g.State != taskengine.GoalWaiting || g.ClaimToken != "" {
		t.Fatalf("goal must release its claim and wait: %+v", g)
	}
	x, err = ts.Execution(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ts.LatestPostCodeReview(ctx, x.Proposal.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale reviewer response was accepted: %v", err)
	}
	effects, err := ts.AttemptEffects(ctx, x.Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	observed := false
	for _, effect := range effects {
		if effect.Action == "provider.review" && effect.State == taskengine.EffectObserved {
			observed = true
		}
	}
	if !observed || held.base.reviews != 1 {
		t.Fatalf("review must be observed once: effects=%+v reviews=%d", effects, held.base.reviews)
	}
	body, err := os.ReadFile(filepath.Join(root, "previous.js"))
	if err != nil || string(body) != ownerBytes {
		t.Fatalf("owner bytes were overwritten: %q, %v", body, err)
	}
}
