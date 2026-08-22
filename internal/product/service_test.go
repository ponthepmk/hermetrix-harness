package product

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
)

func testProductService(t *testing.T) (*Service, *skills.Service, *store.Store) {
	t.Helper()
	dataStore, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })
	skillService := skills.NewService(dataStore)
	return NewService(dataStore, skillService), skillService, dataStore
}

func TestProjectArtifactSettingsAndExplicitMemorySafety(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evidence.txt"), []byte("verified"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Evidence", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	files, err := service.BrowseProject(ctx, project.ID, ".")
	if err != nil || len(files) != 1 || files[0].Name != "evidence.txt" {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	if _, err := service.BrowseProject(ctx, project.ID, "../"); err == nil {
		t.Fatal("project root escape was accepted")
	}
	artifact, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "proof.md", Kind: "report",
		MIMEType: "text/markdown", Content: "# Proof\n\nVerified."})
	if err != nil {
		t.Fatal(err)
	}
	_, data, err := service.GetArtifact(ctx, artifact.ID)
	if err != nil || string(data) != "# Proof\n\nVerified." {
		t.Fatalf("artifact=%q err=%v", data, err)
	}
	if _, err := service.SaveSetting(ctx, "provider.token", map[string]any{"token": "must-not-persist"}); err == nil {
		t.Fatal("secret-like setting was persisted")
	}
	if _, err := service.SaveSetting(ctx, "ui.theme", map[string]any{"mode": "galaxy"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "preference",
		Content: "ตอบภาษาไทย", Source: "agent"}); err == nil {
		t.Fatal("implicit agent memory became active")
	}
	memory, err := service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "preference",
		Content: "ตอบภาษาไทย", Source: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ArchiveMemory(ctx, memory.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchFileOptimisticWriteAndAuditReceipt(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	if err := os.WriteFile(path, []byte("# Before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Workbench", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	document, err := service.ReadProjectFile(ctx, project.ID, "notes.md")
	if err != nil || document.SHA256 == "" || document.Content != "# Before\n" {
		t.Fatalf("document=%+v err=%v", document, err)
	}
	result, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "notes.md", Content: "# After\n",
		ExpectedSHA256: document.SHA256, Actor: "test-user"})
	if err != nil || result.Document.Content != "# After\n" || result.ReceiptArtifact.ID == "" || !strings.Contains(result.Diff, "-# Before") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "notes.md", Content: "stale",
		ExpectedSHA256: document.SHA256, Actor: "test-user"}); err == nil {
		t.Fatal("stale editor write was accepted")
	}
	if _, err := service.WriteProjectFile(ctx, project.ID, WriteFileInput{Path: "../escape.txt", Content: "escape", Actor: "test-user"}); err == nil {
		t.Fatal("project escape write was accepted")
	}
}

func TestBackgroundCommandIsDirectBoundedAuditableAndCancelable(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Commands", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "sh",
		Arguments: []string{"-c", "echo unsafe"}}); err == nil {
		t.Fatal("shell executable was accepted")
	}
	job, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "python3",
		Arguments: []string{"-c", "print('direct-command-ok')"}, TimeoutSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, service, job.ID)
	if completed.State != "completed" || completed.Result["exit_code"] != float64(0) ||
		!strings.Contains(completed.Result["output"].(string), "direct-command-ok") {
		t.Fatalf("completed job=%+v", completed)
	}
	artifactID, _ := completed.Result["artifact_id"].(string)
	if artifactID == "" {
		t.Fatal("command output was not persisted as an artifact")
	}
	longJob, err := service.StartCommand(ctx, CommandInput{ProjectID: project.ID, Actor: "user", Executable: "python3",
		Arguments: []string{"-c", "import time; time.sleep(30)"}, TimeoutSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, _ := service.GetJob(ctx, longJob.ID)
		if current.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never started: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := service.CancelJob(ctx, longJob.ID); err != nil {
		t.Fatal(err)
	}
	canceled := waitForJob(t, service, longJob.ID)
	if canceled.State != "canceled" || !canceled.CancelRequested {
		t.Fatalf("canceled job=%+v", canceled)
	}
}

func TestMinimalEnvironmentProvidesGoCachesWithoutLeakingCredentials(t *testing.T) {
	t.Setenv("HERMETRIX_TEST_SECRET", "must-not-leak")
	environment := minimalEnvironment()
	joined := strings.Join(environment, "\n")
	for _, required := range []string{"GOPATH=", "GOMODCACHE=", "GOCACHE="} {
		if !strings.Contains(joined, required) {
			t.Fatalf("minimal environment lacks %s: %v", required, environment)
		}
	}
	if strings.Contains(joined, "HERMETRIX_TEST_SECRET") || strings.Contains(joined, "must-not-leak") {
		t.Fatal("minimal command environment leaked an unrelated credential")
	}
}

func TestBackupIntegrityConflictPreviewAndCandidateOnlyRestore(t *testing.T) {
	service, skillService, _ := testProductService(t)
	ctx := context.Background()
	markdown := "---\nname: backup-skill\ndescription: \"Repeatable backup evidence procedure\"\ntags: [backup]\ntools: [filesystem.read]\n---\n\n# Procedure\n\n1. Verify the evidence.\n"
	candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "backup-skill", ScopeKind: "user",
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "user", TriggerKind: "manual",
		Reason: "backup test", EvidenceRefs: []string{"session:test"}, Markdown: markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skillService.PromoteCandidate(ctx, candidate.ID, "user", candidate.Revision); err != nil {
		t.Fatal(err)
	}
	run, data, err := service.ExportBackup(ctx, "user")
	if err != nil || run.State != "completed" || len(data) == 0 {
		t.Fatalf("run=%+v size=%d err=%v", run, len(data), err)
	}
	var tampered map[string]any
	if err := json.Unmarshal(data, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["payload_checksum"] = strings.Repeat("0", 64)
	bad, _ := json.Marshal(tampered)
	if _, err := service.PreviewImport(ctx, bad, "user"); err == nil {
		t.Fatal("tampered backup passed preview")
	}
	preview, err := service.PreviewImport(ctx, data, "user")
	if err != nil {
		t.Fatal(err)
	}
	if preview.SkillConflicts != 1 || preview.State != "awaiting_apply" {
		t.Fatalf("preview=%+v", preview)
	}
	result, err := service.ApplyImport(ctx, preview.ID, "user")
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 1 || len(result.CandidateIDs) != 1 || result.State != "imported" {
		t.Fatalf("result=%+v", result)
	}
	active, err := skillService.ListSkills(ctx, false)
	if err != nil || len(active) != 1 {
		t.Fatalf("import mutated active skills: %+v err=%v", active, err)
	}
	restored, err := skillService.GetCandidate(ctx, result.CandidateIDs[0])
	if err != nil || restored.Origin != "imported" || !strings.Contains(restored.Reason, "conflict") {
		t.Fatalf("restored candidate=%+v err=%v", restored, err)
	}
	if _, err := service.ApplyImport(ctx, preview.ID, "user"); err == nil {
		t.Fatal("same import preview was applied twice")
	}
}

func waitForJob(t *testing.T, service *Service, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		item, err := service.GetJob(context.Background(), id)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if item.State == "completed" || item.State == "failed" || item.State == "canceled" || item.State == "interrupted" {
			return item
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not finish: %+v", id, item)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
