package curator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

func setupCurator(t *testing.T) (*Service, *skills.Service, *store.Store) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	skillService := skills.NewService(dataStore)
	return NewService(dataStore, skillService), skillService, dataStore
}

func TestCuratorIsReportOnlyAndRecordsVersionedInput(t *testing.T) {
	service, skillService, _ := setupCurator(t)
	ctx := context.Background()
	markdown := func(name string) string {
		return "---\nname: " + name + "\ndescription: \"Review changes with evidence and report risks\"\ntags: [review]\ntools: []\n---\n\n# Procedure\n\nRead changed files and report risks with evidence.\n"
	}
	var activeIDs []string
	for _, name := range []string{"curator-left", "curator-right"} {
		candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: name,
			ScopeKind: "user", Origin: "user_created", Owner: "user", ChangeKind: "create",
			CreatedBy: "user", TriggerKind: "manual", Reason: "curator test", Markdown: markdown(name)})
		if err != nil {
			t.Fatal(err)
		}
		active, err := skillService.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision)
		if err != nil {
			t.Fatal(err)
		}
		activeIDs = append(activeIDs, active.CurrentVersionID)
	}
	run, err := service.RunReportOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "completed" || run.FindingsCount != 1 || len(run.InputSnapshot) != 2 {
		t.Fatalf("run = %+v", run)
	}
	for _, snapshot := range run.InputSnapshot {
		if snapshot.VersionID == "" {
			t.Fatal("curator input lacks version binding")
		}
	}
	active, _ := skillService.ListSkills(ctx, false)
	if len(active) != 2 {
		t.Fatal("curator changed active skill count")
	}
	for index, skill := range active {
		found := false
		for _, versionID := range activeIDs {
			if skill.CurrentVersionID == versionID {
				found = true
			}
		}
		if !found {
			t.Fatalf("curator mutated version at %d: %+v", index, skill)
		}
	}
	runs, err := service.ListRuns(ctx, 10)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	findings, err := service.ListFindings(ctx, run.ID)
	if err != nil || len(findings) != 1 || findings[0].Proposal["automatic_mutation"] != false {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	if findings[0].Proposal["left_version_id"] == "" || findings[0].Proposal["right_version_id"] == "" {
		t.Fatal("consolidation proposal was not bound to exact versions")
	}
}

func TestCuratorStaleScoreIsEvidenceOnlyAndNeverArchives(t *testing.T) {
	service, skillService, dataStore := setupCurator(t)
	ctx := context.Background()
	markdown := "---\nname: stale-skill\ndescription: \"Old verified process retained for curator testing\"\ntags: [old]\ntools: []\n---\n\n# Procedure\n\n1. Verify old evidence.\n"
	candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "stale-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "user", TriggerKind: "manual",
		Reason: "stale test", Markdown: markdown})
	if err != nil {
		t.Fatal(err)
	}
	active, err := skillService.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-120 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := dataStore.DB.Exec(`UPDATE skills SET updated_at=? WHERE id=?`, old, active.ID); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunReportOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := service.ListFindings(ctx, run.ID)
	if err != nil || len(findings) != 1 || findings[0].Kind != "stale" || findings[0].Score < 0.6 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	stillActive, err := skillService.GetSkill(ctx, active.ID)
	if err != nil || stillActive.State != skills.StateActive || stillActive.CurrentVersionID != active.CurrentVersionID {
		t.Fatalf("curator mutated stale skill: %+v err=%v", stillActive, err)
	}
}

func TestConfiguredCuratorAuthorityArchivesOnlyAgentSkillAndRestoresAsCandidate(t *testing.T) {
	service, skillService, dataStore := setupCurator(t)
	ctx := context.Background()
	policy, err := skillService.SaveAuthorityPolicy(ctx, skills.SaveAuthorityPolicyInput{Mode: skills.AuthorityGated,
		AutoArchiveAgentSkills: true, AllowedScopes: []string{"user"}, MaxCandidateTokens: 4096,
		Actor: "test-user", Reason: "exercise reversible curator gate", ExpectedRevision: 1})
	if err != nil || policy.Revision != 2 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	markdown := "---\nname: stale-agent-skill\ndescription: \"Old agent procedure retained for authority testing\"\ntags: [old]\ntools: []\n---\n\n# Procedure\n\n1. Verify old evidence.\n"
	candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "stale-agent-skill",
		ScopeKind: "user", Origin: "agent_candidate", Owner: "agent", ChangeKind: "create",
		CreatedBy: "background_reviewer", TriggerKind: "successful_milestone", Reason: "stale test", Markdown: markdown})
	if err != nil {
		t.Fatal(err)
	}
	active, err := skillService.PromoteCandidate(ctx, candidate.ID, "test-user", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		_, err = skillService.RecordActivation(ctx, skills.ActivationInput{SessionID: "test-session", TurnID: "failure-turn",
			SkillID: active.ID, VersionID: active.CurrentVersionID, SelectionSource: "test", MetadataExposed: true,
			BodyInjected: true, Outcome: "failure", OutcomeSource: "test", AttributionKind: "explicit"})
		if err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().Add(-120 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := dataStore.DB.Exec(`UPDATE skills SET updated_at=? WHERE id=?`, old, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.DB.Exec(`UPDATE skill_activations SET created_at=?,completed_at=? WHERE skill_id=?`, old, old, active.ID); err != nil {
		t.Fatal(err)
	}
	run, err := service.RunReportOnly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	findings, err := service.ListFindings(ctx, run.ID)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	if findings[0].Score < 0.8 {
		t.Fatalf("stale score did not reach authority threshold: %+v", findings[0])
	}
	current, _ := skillService.GetSkill(ctx, active.ID)
	currentPolicy, _ := skillService.GetAuthorityPolicy(ctx)
	if current.Origin != "agent_promoted" || current.ScopeKind != "user" || current.Pinned || current.Protected ||
		current.CurrentVersionID != findings[0].Evidence["exact_version_id"] || currentPolicy.Mode != skills.AuthorityGated || !currentPolicy.AutoArchiveAgentSkills {
		t.Fatalf("gate inputs skill=%+v policy=%+v finding=%+v", current, currentPolicy, findings[0])
	}
	actions, err := service.ApplyConfiguredAuthority(ctx, run.ID)
	if err != nil || len(actions) != 1 || actions[0].ArchiveID == "" {
		t.Fatalf("actions=%+v err=%v", actions, err)
	}
	archived, err := skillService.GetSkill(ctx, active.ID)
	if err != nil || archived.State != skills.StateArchived {
		t.Fatalf("archived=%+v err=%v", archived, err)
	}
	restore, err := skillService.CreateAuthorityRollback(ctx, actions[0].ID, "test-user", "restore for verification")
	if err != nil || restore.ChangeKind != "restore" || restore.TargetSkillID != active.ID {
		t.Fatalf("restore=%+v err=%v", restore, err)
	}
}

func TestGCDryRunStaleGuardQuarantineAndRestore(t *testing.T) {
	service, _, dataStore := setupCurator(t)
	ctx := context.Background()
	orphanRef, err := dataStore.Blobs.Put([]byte("unreferenced-object"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.DryRunGC(ctx)
	if err != nil || first.UnreachableCount != 1 || !dataStore.Blobs.Exists(orphanRef) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	secondOrphan, err := dataStore.Blobs.Put([]byte("new-object-after-snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyGC(ctx, first.ID, "user"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale apply error=%v", err)
	}
	if !dataStore.Blobs.Exists(orphanRef) || !dataStore.Blobs.Exists(secondOrphan) {
		t.Fatal("stale GC changed the CAS")
	}
	fresh, err := service.DryRunGC(ctx)
	if err != nil || fresh.UnreachableCount != 2 {
		t.Fatalf("fresh=%+v err=%v", fresh, err)
	}
	applied, err := service.ApplyGC(ctx, fresh.ID, "user")
	if err != nil || applied.State != "quarantined" || dataStore.Blobs.Exists(orphanRef) || dataStore.Blobs.Exists(secondOrphan) {
		t.Fatalf("applied=%+v err=%v", applied, err)
	}
	// A partial rollback may leave the same recoverable quarantine with a
	// partial_quarantine state. Restore must converge both states to restored.
	if _, err := dataStore.DB.ExecContext(ctx, `UPDATE gc_runs SET state='partial_quarantine' WHERE id=?`, fresh.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := service.RestoreGC(ctx, fresh.ID, "user")
	if err != nil || restored.State != "restored" || !dataStore.Blobs.Exists(orphanRef) || !dataStore.Blobs.Exists(secondOrphan) {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
	persisted, err := service.GetGCRun(ctx, fresh.ID)
	if err != nil || persisted.State != "restored" {
		t.Fatalf("persisted restore state=%+v err=%v", persisted, err)
	}
}

func TestMaintenanceScheduleHonorsIdleAndPowerPolicy(t *testing.T) {
	service, _, _ := setupCurator(t)
	ctx := context.Background()
	schedule, err := service.SaveSchedule(ctx, ScheduleInput{Name: "Weekly curator", TaskKind: "curator", IntervalSeconds: 300,
		Enabled: true, RequireIdle: true, RequireACPower: true, NextRunAt: time.Now().UTC().Add(-time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	executions, err := service.RunDue(ctx, SystemState{Idle: false, OnPower: true})
	if err != nil || len(executions) != 1 || executions[0].Skipped == "" {
		t.Fatalf("skipped=%+v err=%v", executions, err)
	}
	executions, err = service.RunDue(ctx, SystemState{Idle: true, OnPower: true})
	if err != nil || len(executions) != 1 || executions[0].ResultID == "" || executions[0].Error != "" {
		t.Fatalf("executed=%+v err=%v", executions, err)
	}
	updated, err := service.GetSchedule(ctx, schedule.ID)
	if err != nil || updated.LastRunAt == nil || !updated.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}

// TestGCRestoresMovedBlobsWhenQuarantineFailsMidway drives the compensating
// rollback for real. The existing quarantine test reaches partial_quarantine by
// writing the state directly, so it never executed this path: a failure after
// some blobs have already moved must put every one of them back.
func TestGCRestoresMovedBlobsWhenQuarantineFailsMidway(t *testing.T) {
	service, _, dataStore := setupCurator(t)
	ctx := context.Background()
	refs := []string{}
	for index := 0; index < 4; index++ {
		ref, err := dataStore.Blobs.Put([]byte(fmt.Sprintf("unreferenced-object-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	planned, err := service.DryRunGC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if planned.UnreachableCount != len(refs) {
		t.Fatalf("dry run found %d unreachable objects, want %d", planned.UnreachableCount, len(refs))
	}

	// Block the last candidate by occupying its quarantine destination with a
	// directory, so renaming a file onto it cannot succeed. Everything before
	// it moves first, which is what makes the rollback observable.
	blocked := planned.Candidates[len(planned.Candidates)-1].Ref
	quarantineRoot := filepath.Join(dataStore.Root, "blobs", "quarantine", planned.ID)
	if err := os.MkdirAll(filepath.Join(quarantineRoot, blocked[:2], blocked[2:]), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := service.ApplyGC(ctx, planned.ID, "user"); err == nil {
		t.Fatal("ApplyGC succeeded even though a blob could not be quarantined")
	}
	for _, ref := range refs {
		if !dataStore.Blobs.Exists(ref) {
			t.Fatalf("blob %s was not restored after the failed apply", ref[:8])
		}
	}
	after, err := service.GetGCRun(ctx, planned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "planned" {
		t.Fatalf("a fully rolled-back apply left the run in state %q, want planned", after.State)
	}
}

// Two applies of the same dry run must not both quarantine, and the CAS must
// survive the race intact. Note what this does NOT reach: the loser is turned
// away by the earlier state and stale-snapshot checks, so the rows-affected
// guard on the final UPDATE stays defence-in-depth with no test behind it.
// Removing that guard does not turn this test red.
func TestConcurrentGCAppliesQuarantineOnlyOnce(t *testing.T) {
	service, _, dataStore := setupCurator(t)
	ctx := context.Background()
	refs := []string{}
	for index := 0; index < 3; index++ {
		ref, err := dataStore.Blobs.Put([]byte(fmt.Sprintf("concurrent-orphan-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
	}
	planned, err := service.DryRunGC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for applier := 0; applier < 2; applier++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, applyErr := service.ApplyGC(ctx, planned.ID, "user")
			results <- applyErr
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for applyErr := range results {
		if applyErr == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of 2 concurrent applies succeeded, want exactly 1", succeeded)
	}
	final, err := service.GetGCRun(ctx, planned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "quarantined" {
		t.Fatalf("concurrent applies left the run in state %q", final.State)
	}
	restored, err := service.RestoreGC(ctx, planned.ID, "user")
	if err != nil || restored.State != "restored" {
		t.Fatalf("restore after concurrent apply failed: state=%+v err=%v", restored, err)
	}
	for _, ref := range refs {
		if !dataStore.Blobs.Exists(ref) {
			t.Fatalf("blob %s was lost across a concurrent apply and restore", ref[:8])
		}
	}
}
