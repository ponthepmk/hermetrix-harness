package taskcoord

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

type rejectedGoalAdapter struct {
	base                *goalAdapter
	mode                string
	remaining, rejected int
}

func (a *rejectedGoalAdapter) StreamChat(ctx context.Context, p providers.Profile, key string, r providers.ChatRequest, emit func(providers.Delta) error) (providers.Completion, error) {
	if r.Tools[0].Function.Name == "submit_changes" && a.remaining > 0 {
		a.remaining--
		a.rejected++
		switch a.mode {
		case "transport":
			return providers.Completion{}, errors.New("transport failed")
		case "truncated":
			return providers.Completion{FinishReason: "length", Content: `{"changes":`}, nil
		default:
			return providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_changes", Arguments: `{"changes":[{"path":"sum.js","unknown_field":"rejected"}]}`}}}, nil
		}
	}
	return a.base.StreamChat(ctx, p, key, r, emit)
}

func TestGoalRejectedCompleteProposalStartsFreshBoundedAttempt(t *testing.T) {
	ctx := context.Background()
	base := &goalAdapter{}
	c, ts, task, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
	a := &rejectedGoalAdapter{base: base, remaining: 1}
	c.providers = providers.NewService(db, a)
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	g = advanceGoalUntilIdle(t, c, ts, g)
	if g.State != taskengine.GoalCompleted || g.AttemptsUsed != 2 || a.rejected != 1 || base.proposals != 1 {
		t.Fatalf("state=%s attempts=%d rejected=%d successful=%d", g.State, g.AttemptsUsed, a.rejected, base.proposals)
	}
	body, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if !strings.Contains(string(body), "a+b") {
		t.Fatal("real command outcome missing")
	}
	var failedAttempts, failedEffects, proposals int
	for query, dest := range map[string]*int{
		"SELECT COUNT(*) FROM task_step_attempts WHERE task_id=? AND state='failed'":                                                                                                     &failedAttempts,
		"SELECT COUNT(*) FROM task_effect_intents WHERE task_id=? AND action='provider.propose' AND state='observed' AND json_extract(receipt_json,'$.failure_code')='invalid_proposal'": &failedEffects,
		"SELECT COUNT(*) FROM task_code_proposals WHERE task_id=?":                                                                                                                       &proposals,
	} {
		if err := db.DB.QueryRowContext(ctx, query, task.ID).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if failedAttempts != 1 || failedEffects != 1 || proposals != 1 {
		t.Fatalf("failed attempts/effects/valid proposals: %d/%d/%d", failedAttempts, failedEffects, proposals)
	}
	if g.Deadline.Sub(g.CreatedAt).Seconds() > float64(input.MaxSeconds)+1 {
		t.Fatal("deadline reset during retry")
	}
}

func TestGoalRejectedProposalsCannotEscapeAttemptBudget(t *testing.T) {
	ctx := context.Background()
	base := &goalAdapter{}
	c, ts, _, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
	a := &rejectedGoalAdapter{base: base, remaining: 20}
	c.providers = providers.NewService(db, a)
	input.MaxAttempts = 2
	before, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	g, err := c.StartGoal(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < input.MaxActions && g.State == taskengine.GoalActive; n++ {
		_, _ = c.AdvanceGoal(ctx, g.ID)
		g, err = ts.Goal(ctx, g.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "sum.js"))
	if g.State != taskengine.GoalBudgetExhausted || g.AttemptsUsed != 2 || a.rejected != 2 || string(before) != string(after) {
		t.Fatalf("state=%s attempts=%d rejected=%d", g.State, g.AttemptsUsed, a.rejected)
	}
}

func TestGoalTransportAndTruncationStillRequireInspection(t *testing.T) {
	for _, mode := range []string{"transport", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			base := &goalAdapter{}
			c, ts, _, input, root, db := newGoalFixtureWithStore(t, base, taskengine.GoalContract{})
			a := &rejectedGoalAdapter{base: base, mode: mode, remaining: 20}
			c.providers = providers.NewService(db, a)
			before, _ := os.ReadFile(filepath.Join(root, "sum.js"))
			g, err := c.StartGoal(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			for n := 0; n < 10 && g.State == taskengine.GoalActive; n++ {
				_, _ = c.AdvanceGoal(ctx, g.ID)
				g, err = ts.Goal(ctx, g.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			after, _ := os.ReadFile(filepath.Join(root, "sum.js"))
			if g.State != taskengine.GoalWaiting || g.AttemptsUsed != 1 || a.rejected != 1 || string(before) != string(after) {
				t.Fatalf("unsafe retry: %+v calls=%d", g, a.rejected)
			}
		})
	}
}
