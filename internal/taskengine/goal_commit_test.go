package taskengine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"hermetrix-harness/internal/identity"
	"hermetrix-harness/internal/store"

	"modernc.org/sqlite"
)

// The barrier delegates every SQL operation to real SQLite. Its only change is
// to let a second connection commit immediately before the final transaction.
// This avoids a production hook and makes the interleaving deterministic.
type goalCommitDriver struct {
	beforeBegin func()
	once        sync.Once
}

func (d *goalCommitDriver) Open(name string) (driver.Conn, error) {
	conn, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &goalCommitConn{Conn: conn, owner: d}, nil
}

type goalCommitConn struct {
	driver.Conn
	owner *goalCommitDriver
}

func (c *goalCommitConn) Begin() (driver.Tx, error) {
	c.owner.once.Do(c.owner.beforeBegin)
	return c.Conn.Begin()
}

func installGoalCommitBarrier(t *testing.T, data *store.Store, before func(*Service)) {
	t.Helper()
	originalDB := data.DB
	other := NewService(&store.Store{DB: originalDB, Root: data.Root, Blobs: data.Blobs})
	driverName := identity.New("goal-commit-barrier")
	sql.Register(driverName, &goalCommitDriver{beforeBegin: func() { before(other) }})
	barrierDB, err := sql.Open(driverName, filepath.Join(data.Root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	barrierDB.SetMaxOpenConns(1)
	data.DB = barrierDB
	t.Cleanup(func() { data.DB = originalDB; _ = barrierDB.Close() })
}

func TestGoalCompletionRechecksEvidenceInsideItsCommit(t *testing.T) {
	for _, mode := range []string{"later_failure", "already_failed", "later_pass", "concurrent_stop"} {
		t.Run(mode, func(t *testing.T) {
			s, data := testService(t)
			ctx := context.Background()
			task := createPlannedTask(t, s)
			for i := range task.Plan.Steps {
				step := task.Plan.Steps[i]
				var err error
				task, err = s.StartStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
				if err != nil {
					t.Fatal(err)
				}
				step = task.Plan.Steps[i]
				for _, check := range step.Checks {
					if _, err = s.RecordValidation(ctx, Validation{TaskID: task.ID, StepID: step.ID, CheckID: check,
						SubjectRevision: StepSubjectRevision(step), Status: ValidationPass, EvidenceRefs: []string{"receipt:" + check}}); err != nil {
						t.Fatal(err)
					}
				}
				task, err = s.CompleteStep(ctx, task.ID, step.Key, task.Revision, step.Revision)
				if err != nil {
					t.Fatal(err)
				}
			}
			criterion := Validation{TaskID: task.ID, RequirementID: "AC-1", CheckID: "acceptance",
				SubjectRevision: RequirementSubjectRevision(task), Status: ValidationPass, EvidenceRefs: []string{"receipt:accepted"}}
			if _, err := s.RecordValidation(ctx, criterion); err != nil {
				t.Fatal(err)
			}
			g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", goalConfig())
			if err != nil {
				t.Fatal(err)
			}
			g, _, err = s.ClaimGoalAction(ctx, g.ID, "finish")
			if err != nil {
				t.Fatal(err)
			}
			task, err = s.CompleteTask(ctx, task.ID, task.Revision)
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.EvaluateCompletion(ctx, task.ID, task.Revision)
			if err != nil {
				t.Fatal(err)
			}

			mutated := false
			failure := mode == "later_failure" || mode == "already_failed"
			if mode == "already_failed" {
				criterion.Status = ValidationFail
				if _, err := s.RecordValidation(ctx, criterion); err != nil {
					t.Fatal(err)
				}
				mutated = true
			}
			installGoalCommitBarrier(t, data, func(other *Service) {
				if mode == "already_failed" {
					return
				} else if mode == "concurrent_stop" {
					if _, err := other.StopGoal(ctx, g.ID); err != nil {
						t.Fatal(err)
					}
				} else {
					criterion.Status = ValidationFail
					if mode == "later_pass" {
						criterion.Status = ValidationPass
					}
					criterion.EvidenceRefs = []string{"receipt:later-observation"}
					if _, err := other.RecordValidation(ctx, criterion); err != nil {
						t.Fatal(err)
					}
				}
				mutated = true
			})

			g, err = s.FinishGoalAction(ctx, g.ID, g.ClaimToken, GoalCompleted, "current checks passed", map[string]any{"outcome": "observed", "completion_subject_hash": before.SubjectHash})
			if !mutated {
				t.Fatal("concurrent evidence mutation was not exercised")
			}
			wantState, wantOutcome := GoalWaiting, "requires_inspection"
			if mode == "later_pass" {
				wantState, wantOutcome = GoalCompleted, "observed"
			}
			if mode == "concurrent_stop" {
				wantState, wantOutcome = GoalStopped, "stopped"
			}
			if g.State != wantState || g.ClaimToken != "" || (failure && !errors.Is(err, ErrStaleRevision)) || (!failure && err != nil) {
				t.Fatalf("commit boundary state=%s claim=%q err=%v", g.State, g.ClaimToken, err)
			}
			actions, err := s.GoalActions(ctx, g.ID, 10)
			if err != nil || len(actions) != 1 || actions[0].State != "observed" || actions[0].Receipt["outcome"] != wantOutcome {
				t.Fatalf("completion must retain an inspectable receipt: %+v err=%v", actions, err)
			}
			if mode == "later_pass" {
				after, err := s.EvaluateCompletion(ctx, task.ID, task.Revision)
				if err != nil || after.SubjectHash == before.SubjectHash || actions[0].Receipt["completion_subject_hash"] != after.SubjectHash || actions[0].Receipt["task_revision"] != float64(task.Revision) {
					t.Fatalf("receipt did not bind evidence actually committed: %+v report=%+v err=%v", actions, after, err)
				}
			} else if _, exists := actions[0].Receipt["completion_subject_hash"]; exists {
				t.Fatal("uncompleted Goal retained a success hash")
			}
			if mode == "concurrent_stop" && g.Reason != "stopped by owner" {
				t.Fatalf("Stop lost its reason: %+v", g)
			}
		})
	}
}

func TestGoalCommitRefusesRequirementChangeDuringActionFinalization(t *testing.T) {
	s, data := testService(t)
	ctx := context.Background()
	task := createPlannedTask(t, s)
	g, err := s.CreateGoal(ctx, task.ID, task.Revision, "owner", goalConfig())
	if err != nil {
		t.Fatal(err)
	}
	g, _, err = s.ClaimGoalAction(ctx, g.ID, "plan")
	if err != nil {
		t.Fatal(err)
	}
	var revised Task
	installGoalCommitBarrier(t, data, func(other *Service) {
		var err error
		revised, err = other.ReviseRequirements(ctx, ReviseRequirementsInput{TaskID: task.ID,
			ExpectedTaskRevision: task.Revision, Actor: "owner",
			Criteria: []Criterion{{ID: "AC-1", Description: "new acceptance evidence required"}}})
		if err != nil {
			t.Fatal(err)
		}
	})
	g, err = s.FinishGoalAction(ctx, g.ID, g.ClaimToken, GoalActive, "plan action settled", nil)
	if !errors.Is(err, ErrStaleRevision) || g.State != GoalWaiting || g.ClaimToken != "" || g.ExpectedTaskRevision != revised.Revision {
		t.Fatalf("changed requirements were silently adopted: %+v revised=%+v err=%v", g, revised, err)
	}
	if _, err := s.ResumeGoal(ctx, g.ID, revised.Revision); err == nil {
		t.Fatal("old requirement consent resumed against a different requirement")
	}
}
