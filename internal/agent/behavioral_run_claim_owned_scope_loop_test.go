package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"hermetrix-harness/internal/skills"
)

func TestBehavioralRunClaimRechecksInactiveOwnedScope(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		desc     string
		table    string
		col      string
		inactive string
		active   string
		setID    string
	}{
		{
			desc:     "inactive owned session",
			table:    "agent_sessions",
			col:      "id",
			inactive: "closed",
			active:   "active",
			setID:    "b.SessionID",
		},
		{
			desc:     "inactive owned project",
			table:    "projects",
			col:      "id",
			inactive: "archived",
			active:   "active",
			setID:    "b.ProjectID",
		},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			service, candidate, b := behavioralRunClaimScopeFixture(t)
			hostID := "host-claim-owned-scope-loop"
			run, err := service.skills.PrepareBehavioralRun(ctx, hostID, candidate.ID, b)
			if err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if run.ID == "" {
				t.Fatalf("prepared run id empty")
			}
			if run.ManifestSHA256 == "" {
				t.Fatalf("prepared manifest sha empty")
			}
			res, err := service.store.DB.ExecContext(ctx, `UPDATE `+c.table+` SET state=? WHERE `+c.col+`=?`, c.inactive, evalID(c.setID, b))
			if err != nil {
				t.Fatalf("recheck inactive exec: %v", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				t.Fatalf("recheck rows affected: %v", err)
			}
			if n != 1 {
				t.Fatalf("recheck rows affected = %d, want 1", n)
			}
			claimed, err := service.skills.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, b)
			if !errors.Is(err, skills.ErrBehavioralRunBindings) {
				t.Fatalf("claim inactive: expected ErrBehavioralRunBindings, got %v", err)
			}
			if !reflect.DeepEqual(claimed, skills.BehavioralRun{}) {
				t.Fatalf("claimed record not zero, got %+v", claimed)
			}
			got, err := service.skills.GetBehavioralRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("get prepared run: %v", err)
			}
			if !reflect.DeepEqual(got, run) {
				t.Fatalf("get != prepared run\n got: %+v\n want: %+v", got, run)
			}
			restored, err := service.store.DB.ExecContext(ctx, `UPDATE `+c.table+` SET state=? WHERE `+c.col+`=?`, c.active, evalID(c.setID, b))
			if err != nil {
				t.Fatalf("restore rows affected: %v", err)
			}
			n, err = restored.RowsAffected()
			if err != nil {
				t.Fatalf("restore rows affected: %v", err)
			}
			if n != 1 {
				t.Fatalf("restore rows affected = %d, want 1", n)
			}
			claimed2, err := service.skills.ClaimBehavioralRun(ctx, run.ID, run.ManifestSHA256, b)
			if err != nil {
				t.Fatalf("claim after restore: %v", err)
			}
			if claimed2.State != skills.BehavioralRunDispatched {
				t.Fatalf("claimed state = %q, want dispatched", claimed2.State)
			}
			if claimed2.ManifestSHA256 != run.ManifestSHA256 {
				t.Fatalf("manifest sha changed")
			}
			var runs int64
			if err := service.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM behavioral_eval_runs`).Scan(&runs); err != nil {
				t.Fatalf("count runs: %v", err)
			}
			if runs != 1 {
				t.Fatalf("behavioral_eval_runs count = %d, want 1", runs)
			}
			var candidates int64
			if err := service.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM candidate_behavioral_evals`).Scan(&candidates); err != nil {
				t.Fatalf("count candidates: %v", err)
			}
			if candidates != 0 {
				t.Fatalf("candidate_behavioral_evals count = %d, want 0", candidates)
			}
		})
	}
}

func evalID(setID string, b skills.BehavioralRunBindings) string {
	if setID == "b.SessionID" {
		return b.SessionID
	}
	return b.ProjectID
}
