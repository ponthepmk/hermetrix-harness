package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

func TestOutputPathsMigration78To79(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Phase 1: create legacy data at the current schema.
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	svc := taskengine.NewService(s)

	task, err := svc.Create(ctx, taskengine.CreateTaskInput{
		Title:           "legacy task",
		Objective:       "test objective",
		OriginalRequest: "do the thing",
		Criteria:        []taskengine.Criterion{{ID: "AC1", Description: "criterion"}},
		Actor:           "owner",
	})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}

	plan, err := svc.CreatePlan(ctx, taskengine.CreatePlanInput{
		TaskID:               task.ID,
		ExpectedTaskRevision: task.Revision,
		RequirementRevision:  task.ActiveRequirementRevision,
		Reason:               "initial",
		Actor:                "owner",
		Steps: []taskengine.StepSpec{{
			Key:            "one",
			Title:          "Step One",
			Instructions:   "instructions",
			RequirementIDs: []string{"AC1"},
			Checks:         []string{"check"},
		}},
	})
	if err != nil {
		s.Close()
		t.Fatal(err)
	}

	taskID := task.ID
	stepID := plan.Plan.Steps[0].ID
	origState := plan.State
	origRev := plan.Revision

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Phase 2: downgrade to schema 78 via raw SQL (real fixture).
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('task_steps') WHERE name='output_paths_json'`).Scan(&n); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if n > 0 {
		if _, err := db.Exec(`ALTER TABLE task_steps DROP COLUMN output_paths_json`); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version=78`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Phase 3: reopen – migration 78→79 must run.
	s2, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if store.CurrentSchemaVersion != 79 {
		s2.Close()
		t.Fatalf("missing schema79: CurrentSchemaVersion=%d, want 79", store.CurrentSchemaVersion)
	}
	ver, err := s2.SchemaVersion(ctx)
	if err != nil {
		s2.Close()
		t.Fatal(err)
	}
	if ver != 79 {
		s2.Close()
		t.Fatalf("missing schema79: version=%d, want 79", ver)
	}

	// Column definition: TEXT NOT NULL DEFAULT '[]'.
	var colType string
	var notNull int
	var dflt string
	if err := s2.DB.QueryRow(`SELECT type, "notnull", dflt_value FROM pragma_table_info('task_steps') WHERE name='output_paths_json'`).Scan(&colType, &notNull, &dflt); err != nil {
		s2.Close()
		t.Fatal(err)
	}
	if colType != "TEXT" || notNull != 1 || dflt != "'[]'" {
		s2.Close()
		t.Fatalf("column def: type=%s notnull=%d default=%s", colType, notNull, dflt)
	}

	// Legacy row preserved with default empty array.
	var title, state, opJSON string
	if err := s2.DB.QueryRow(`SELECT title, state, output_paths_json FROM task_steps WHERE id=?`, stepID).Scan(&title, &state, &opJSON); err != nil {
		s2.Close()
		t.Fatal(err)
	}
	if title != "Step One" || state != "pending" {
		s2.Close()
		t.Fatalf("row: title=%q state=%q want Step One/pending", title, state)
	}
	var paths []string
	if err := json.Unmarshal([]byte(opJSON), &paths); err != nil || len(paths) != 0 {
		s2.Close()
		t.Fatalf("output_paths_json=%q want []", opJSON)
	}

	// Public engine Get: OutputPaths nil/empty, revision unchanged.
	svc2 := taskengine.NewService(s2)
	got, err := svc2.Get(ctx, taskID)
	if err != nil {
		s2.Close()
		t.Fatal(err)
	}
	if got.Revision != origRev {
		s2.Close()
		t.Fatalf("revision %d→%d", origRev, got.Revision)
	}
	if got.State != origState {
		s2.Close()
		t.Fatalf("state %q->%q", origState, got.State)
	}
	if len(got.Plan.Steps) != 1 {
		s2.Close()
		t.Fatalf("steps=%d want 1", len(got.Plan.Steps))
	}
	if p := got.Plan.Steps[0].OutputPaths; p != nil && len(p) != 0 {
		s2.Close()
		t.Fatalf("OutputPaths=%v want nil/empty", p)
	}

	// FK integrity.
	var fk int
	if err := s2.DB.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fk); err != nil {
		s2.Close()
		t.Fatal(err)
	}
	if fk != 0 {
		s2.Close()
		t.Fatalf("fk violations=%d", fk)
	}

	if err := s2.Close(); err != nil {
		t.Fatal(err)
	}

	// Phase 4: idempotent reopen – schema stays 79, one column, value intact.
	s3, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s3.Close()

	ver3, err := s3.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ver3 != 79 {
		t.Fatalf("reopen version=%d want 79", ver3)
	}
	var n2 int
	if err := s3.DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('task_steps') WHERE name='output_paths_json'`).Scan(&n2); err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("column count=%d want 1", n2)
	}
	var op2 string
	if err := s3.DB.QueryRow(`SELECT output_paths_json FROM task_steps WHERE id=?`, stepID).Scan(&op2); err != nil {
		t.Fatal(err)
	}
	if op2 != "[]" {
		t.Fatalf("output_paths_json=%q want []", op2)
	}
}
