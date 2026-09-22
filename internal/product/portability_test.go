package product

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptedWorkspaceMigrationRestoresPrivateCanonicalContentOnly(t *testing.T) {
	ctx := context.Background()
	source, _, sourceStore := testProductService(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("private workspace text"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := source.SaveProject(ctx, ProjectInput{Name: "Portable Project", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := source.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "private.bin", Kind: "private_evidence", MIMEType: "application/octet-stream", Content: "private artifact bytes"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "decision", Content: "private memory", Source: "user"}); err != nil {
		t.Fatal(err)
	}
	passphrase := "correct horse battery staple"
	exportJob, encrypted, err := source.ExportWorkspace(ctx, WorkspaceExportInput{ProjectIDs: []string{project.ID}, Passphrase: passphrase, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if exportJob.State != "completed" || bytes.Contains(encrypted, []byte("private workspace text")) || bytes.Contains(encrypted, []byte("private artifact bytes")) {
		t.Fatalf("export is not opaque: job=%+v", exportJob)
	}
	var leaked int
	if err = sourceStore.DB.QueryRow(`SELECT COUNT(*) FROM workspace_migration_jobs WHERE summary_json LIKE ? OR error LIKE ?`, `%`+passphrase+`%`, `%`+passphrase+`%`).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("passphrase persistence count=%d err=%v", leaked, err)
	}

	destination, _, destinationStore := testProductService(t)
	if _, err = destination.PreviewWorkspaceImport(ctx, WorkspaceImportPreviewInput{EncryptedPackage: encrypted, Passphrase: "wrong passphrase value", Actor: "owner"}); err == nil {
		t.Fatal("wrong passphrase decrypted package")
	}
	tampered := append([]byte(nil), encrypted...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err = destination.PreviewWorkspaceImport(ctx, WorkspaceImportPreviewInput{EncryptedPackage: tampered, Passphrase: passphrase, Actor: "owner"}); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
	preview, err := destination.PreviewWorkspaceImport(ctx, WorkspaceImportPreviewInput{EncryptedPackage: encrypted, Passphrase: passphrase, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	destinationRoot := filepath.Join(t.TempDir(), "restored")
	applied, err := destination.ApplyWorkspaceImport(ctx, WorkspaceImportApplyInput{PreviewID: preview.ID, Passphrase: passphrase, Actor: "owner", RootMappings: map[string]string{project.ID: destinationRoot}})
	if err != nil {
		t.Fatal(err)
	}
	if applied.State != "completed" || applied.Summary["authority_imported"] != false {
		t.Fatalf("apply=%+v", applied)
	}
	body, err := os.ReadFile(filepath.Join(destinationRoot, "notes.txt"))
	if err != nil || string(body) != "private workspace text" {
		t.Fatalf("restored file=%q err=%v", body, err)
	}
	projects, err := destination.ListProjects(ctx)
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects=%v err=%v", projects, err)
	}
	artifacts, err := destination.ListArtifacts(ctx, projects[0].ID)
	if err != nil || len(artifacts) != 1 || artifacts[0].Visibility != "private" || artifacts[0].ExportPolicy != "deny" || artifacts[0].Checksum != artifact.Checksum {
		t.Fatalf("artifacts=%v err=%v", artifacts, err)
	}
	memories, err := destination.ListMemories(ctx, "project", projects[0].ID)
	if err != nil || len(memories) != 1 || memories[0].Content != "private memory" || memories[0].Visibility != "private" {
		t.Fatalf("memories=%v err=%v", memories, err)
	}
	for _, table := range []string{"tool_approvals", "task_effect_intents", "task_step_attempts", "agent_sessions"} {
		var count int
		if err = destinationStore.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("authority table %s count=%d err=%v", table, count, err)
		}
	}
}
