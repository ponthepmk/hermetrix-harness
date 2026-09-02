package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationFromV1AddsLearningAgentAndBindingTables(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var schemaVersion int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&schemaVersion); err != nil || schemaVersion != CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", schemaVersion, err)
	}
	// A version number proves nothing on its own. Check that the newest
	// migration actually added its columns.
	for _, column := range []string{"reasoning_ratio", "reasoning_sample"} {
		var found int
		if err := dataStore.DB.QueryRow(
			`SELECT COUNT(*) FROM pragma_table_info('provider_profiles') WHERE name=?`, column).Scan(&found); err != nil || found != 1 {
			t.Fatalf("provider_profiles is missing column %s: found=%d err=%v", column, found, err)
		}
	}
	for _, table := range []string{"learning_reviews", "learning_trigger_outbox", "curator_runs", "provider_profiles", "agent_sessions", "agent_events", "context_snapshots", "event_embeddings", "step_bindings", "tool_approvals", "mcp_servers", "mcp_tools", "skill_replay_runs", "skill_replay_cases", "candidate_capability_reviews", "context_eval_cases", "context_eval_runs", "model_qualification_runs", "projects", "artifacts", "background_jobs", "settings", "memories", "backup_runs", "curator_findings", "maintenance_schedules", "gc_runs", "skill_authority_policy", "skill_authority_actions", "terminal_sessions", "browser_tabs", "agent_teams", "agent_team_members", "agent_team_runs", "agent_team_tasks"} {
		var found string
		if err := dataStore.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil {
			t.Fatalf("missing %s: %v", table, err)
		}
	}
	var sourceColumn int
	rows, err := dataStore.DB.Query(`PRAGMA table_info(skill_candidates)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "source_review_id" {
			sourceColumn++
		}
	}
	if sourceColumn != 1 {
		t.Fatalf("source_review_id columns = %d", sourceColumn)
	}
	var toolBindingsColumn int
	stepRows, err := dataStore.DB.Query(`PRAGMA table_info(step_bindings)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stepRows.Close()
	for stepRows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := stepRows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "tool_bindings_json" {
			toolBindingsColumn++
		}
	}
	if toolBindingsColumn != 1 {
		t.Fatalf("tool_bindings_json columns = %d", toolBindingsColumn)
	}
	var version int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, err=%v", version, err)
	}
}

func TestMigrationV26BackfillsFrozenTeamAndMemberInstructions(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schemaV25); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO agent_teams(id,project_id,name,instructions,state,revision,created_at,updated_at) VALUES('team-1',NULL,'Original team','Original rules','active',1,'now','now')`,
		`INSERT INTO agent_team_members(id,team_id,name,role,instructions,is_lead,sort_order,created_at) VALUES('member-1','team-1','Lead','synthesis','Original member rules',1,0,'now')`,
		`INSERT INTO agent_team_runs(id,team_id,project_id,objective,provider_id,context_profile,state,max_parallel,actor,created_at) VALUES('run-1','team-1',NULL,'Objective','provider','certified-64k','completed',1,'test','now')`,
		`INSERT INTO agent_team_tasks(id,run_id,member_id,title,prompt,depends_json,state,created_at) VALUES('task-1','run-1','member-1','Synthesis','Prompt','[]','completed','now')`,
		`PRAGMA user_version=25`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var teamName, teamInstructions, memberName, memberRole, memberInstructions, memberState string
	if err := dataStore.DB.QueryRow(`SELECT team_name,team_instructions FROM agent_team_runs WHERE id='run-1'`).Scan(&teamName, &teamInstructions); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DB.QueryRow(`SELECT member_name,member_role,member_instructions FROM agent_team_tasks WHERE id='task-1'`).Scan(&memberName, &memberRole, &memberInstructions); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DB.QueryRow(`SELECT state FROM agent_team_members WHERE id='member-1'`).Scan(&memberState); err != nil {
		t.Fatal(err)
	}
	if teamName != "Original team" || teamInstructions != "Original rules" || memberName != "Lead" ||
		memberRole != "synthesis" || memberInstructions != "Original member rules" || memberState != "active" {
		t.Fatalf("v26 backfill lost execution provenance: team=%q/%q member=%q/%q/%q state=%q",
			teamName, teamInstructions, memberName, memberRole, memberInstructions, memberState)
	}
	for table, columns := range map[string][]string{
		"agent_team_runs":  {"qualification_reason"},
		"agent_team_tasks": {"approval_id", "approval_summary", "approval_preview", "approval_effect"},
	} {
		for _, column := range columns {
			var found int
			if err := dataStore.DB.QueryRow(
				`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&found); err != nil || found != 1 {
				t.Fatalf("v27 migration is missing %s.%s: found=%d err=%v", table, column, found, err)
			}
		}
	}
	var qualificationReason, approvalID, approvalSummary, approvalPreview, approvalEffect string
	if err := dataStore.DB.QueryRow(`SELECT qualification_reason FROM agent_team_runs WHERE id='run-1'`).Scan(&qualificationReason); err != nil {
		t.Fatal(err)
	}
	if err := dataStore.DB.QueryRow(`SELECT approval_id,approval_summary,approval_preview,approval_effect
		FROM agent_team_tasks WHERE id='task-1'`).Scan(&approvalID, &approvalSummary, &approvalPreview, &approvalEffect); err != nil {
		t.Fatal(err)
	}
	if qualificationReason != "" || approvalID != "" || approvalSummary != "" || approvalPreview != "" || approvalEffect != "" {
		t.Fatalf("v27 defaults are not empty: qualification=%q approval=%q/%q/%q/%q",
			qualificationReason, approvalID, approvalSummary, approvalPreview, approvalEffect)
	}
}

func TestMigrationFromPreReleaseV11AcceptsQualificationColumnsAlreadyPresent(t *testing.T) {
	root := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(root, "hermetrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{schemaV1, schemaV2, schemaV3, schemaV4, schemaV5, schemaV6, schemaV7, schemaV8, schemaV9,
		schemaV10, schemaV11} {
		if _, err := db.Exec(schema); err != nil {
			t.Fatal(err)
		}
	}
	// Reproduce the interim schema-11 shape that already had the V12 fields.
	for _, statement := range []string{
		`ALTER TABLE model_qualification_runs ADD COLUMN requested_profile TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE model_qualification_runs ADD COLUMN eligible INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE model_qualification_runs ADD COLUMN requires_decision INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`PRAGMA user_version=11`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	dataStore, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()
	var version int
	if err := dataStore.DB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
}

// TestSchemaV29AllowsProjectsWithoutCode pins the change that makes a project a
// bounded scope rather than a code folder: several projects may have no root at
// all, while two projects still cannot claim the same one.
func TestSchemaV29AllowsProjectsWithoutCode(t *testing.T) {
	store, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	insert := func(id, name, root string) error {
		_, err := store.DB.Exec(`INSERT INTO projects(id,name,root_path,created_at,updated_at)
      VALUES(?,?,?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, id, name, root)
		return err
	}
	if err := insert("p1", "life", ""); err != nil {
		t.Fatalf("a project with no code root was rejected: %v", err)
	}
	if err := insert("p2", "notes", ""); err != nil {
		t.Fatalf("a second project with no code root was rejected: %v", err)
	}
	if err := insert("p3", "code", "/tmp/one"); err != nil {
		t.Fatal(err)
	}
	if err := insert("p4", "same", "/tmp/one"); err == nil {
		t.Error("two projects claimed the same root")
	}
	var pinned int
	var opened sql.NullString
	if err := store.DB.QueryRow(`SELECT pinned,last_opened_at FROM projects WHERE id='p1'`).
		Scan(&pinned, &opened); err != nil {
		t.Fatalf("picker columns are missing: %v", err)
	}
}

// TestOrphanSessionsLandInInbox covers the migration's other half. Sessions may
// have had no project at all ("chat only"); once a project is the root of
// everything they would have nowhere to live. None of them may be hidden or
// dropped, so they move to an ordinary project the user can rename or delete.
func TestOrphanSessionsLandInInbox(t *testing.T) {
	root := t.TempDir()
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO provider_profiles
    (id,name,adapter_kind,base_url,model,api_key_env,context_window,context_evidence,
     max_output_tokens,enabled,created_at,updated_at)
    VALUES('pr','P','openai-compatible','https://h.example/v1','m','',131072,'declared',4096,1,
    '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO agent_sessions
    (id,title,provider_id,context_profile,state,project_id,created_at,updated_at)
    VALUES('s1','Orphan','pr','compact-32k','idle',NULL,
    '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Reopening runs the migration against a database that already has rows.
	reopened, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var project, name, rootPath string
	if err := reopened.DB.QueryRow(`SELECT s.project_id,p.name,p.root_path
    FROM agent_sessions s JOIN projects p ON p.id=s.project_id WHERE s.id='s1'`).
		Scan(&project, &name, &rootPath); err != nil {
		t.Fatalf("the orphan session has no project: %v", err)
	}
	if name != "Inbox" || rootPath != "" {
		t.Errorf("orphan landed in %q (root %q), want the rootless Inbox", name, rootPath)
	}
}
