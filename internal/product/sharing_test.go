package product

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	"hermetrix-harness/internal/taskengine"
)

func TestSharePreviewDetectsTOCTOUAndImportsPrivateContent(t *testing.T) {
	service, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("version one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=secret"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Share Source", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "project", ObjectID: project.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: project.SharingRevision, Actor: "owner", Reason: "share selected files"}); err != nil {
		t.Fatal(err)
	}
	artifact, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "evidence.json", Kind: "evidence", MIMEType: "application/json", Content: `{"ok":true}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "artifact", ObjectID: artifact.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: artifact.SharingRevision, Actor: "owner", Reason: "share evidence"}); err != nil {
		t.Fatal(err)
	}
	preview, err := service.CreateSharePreview(ctx, SharePreviewInput{ProjectID: project.ID, Paths: []string{"main.txt", ".env"}, ArtifactIDs: []string{artifact.ID}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 2 || len(preview.Omitted) != 1 {
		t.Fatalf("preview=%+v", preview)
	}
	if err = os.WriteFile(filepath.Join(root, "main.txt"), []byte("changed after preview"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ExportShare(ctx, ShareExportInput{ProjectID: project.ID, PreviewID: preview.ID, ManifestDigest: preview.ManifestDigest, IdempotencyKey: "export-one"}); err == nil {
		t.Fatal("source drift was accepted")
	}

	if err = os.WriteFile(filepath.Join(root, "main.txt"), []byte("stable version"), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err = service.CreateSharePreview(ctx, SharePreviewInput{ProjectID: project.ID, Paths: []string{"main.txt"}, ArtifactIDs: []string{artifact.ID}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	exported, err := service.ExportShare(ctx, ShareExportInput{ProjectID: project.ID, PreviewID: preview.ID, ManifestDigest: preview.ManifestDigest, IdempotencyKey: "export-two"})
	if err != nil {
		t.Fatal(err)
	}
	_, packageData, err := service.ShareExportContent(ctx, exported.ID)
	if err != nil {
		t.Fatal(err)
	}
	importPreview, err := service.PreviewShareImport(ctx, packageData, "owner")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "imported-project")
	imported, err := service.ApplyShareImport(ctx, ApplyShareImportInput{PreviewID: importPreview.ID, ManifestDigest: importPreview.ManifestDigest,
		ProjectName: "Imported Share", DestinationRoot: destination, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "main.txt"))
	if err != nil || string(body) != "stable version" {
		t.Fatalf("imported file=%q err=%v", body, err)
	}
	artifacts, err := service.ListArtifacts(ctx, imported.ID)
	if err != nil || len(artifacts) != 1 {
		t.Fatalf("artifacts=%v err=%v", artifacts, err)
	}
	if artifacts[0].Visibility != "private" || artifacts[0].ExportPolicy != "deny" {
		t.Fatalf("imported artifact policy=%+v", artifacts[0])
	}
}

func TestShareRejectsPrivateDerivationAndMaliciousArchives(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Private Dependency", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "project", ObjectID: project.ID, Visibility: "project_shared", ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	source, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "private", Kind: "source", MIMEType: "text/plain", Content: "private"})
	if err != nil {
		t.Fatal(err)
	}
	derived, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "derived", Kind: "derived", MIMEType: "text/plain", Content: "derived"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "artifact", ObjectID: derived.ID, Visibility: "project_shared", ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	ownerID, _ := dataStore.OwnerPrincipalID(ctx)
	if _, err = dataStore.DB.Exec(`INSERT INTO artifact_derivations(id,owner_principal_id,source_artifact_id,result_artifact_id,relation_kind,source_hash,result_hash,created_at) VALUES(?,?,?,?,?,?,?,datetime('now'))`, "derive-test", ownerID, source.ID, derived.ID, "derived", source.Checksum, derived.Checksum); err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateSharePreview(ctx, SharePreviewInput{ProjectID: project.ID, ArtifactIDs: []string{derived.ID}, Actor: "owner"}); err == nil {
		t.Fatal("private transitive dependency was exported")
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "artifact", ObjectID: source.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "share reviewed source lineage"}); err != nil {
		t.Fatal(err)
	}
	lineagePreview, err := service.CreateSharePreview(ctx, SharePreviewInput{ProjectID: project.ID, ArtifactIDs: []string{derived.ID}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	lineageExport, err := service.ExportShare(ctx, ShareExportInput{ProjectID: project.ID, PreviewID: lineagePreview.ID,
		ManifestDigest: lineagePreview.ManifestDigest, IdempotencyKey: "lineage-export"})
	if err != nil {
		t.Fatal(err)
	}
	_, lineagePackage, err := service.ShareExportContent(ctx, lineageExport.ID)
	if err != nil {
		t.Fatal(err)
	}
	lineageImport, err := service.PreviewShareImport(ctx, lineagePackage, "owner")
	if err != nil {
		t.Fatal(err)
	}
	importedProject, err := service.ApplyShareImport(ctx, ApplyShareImportInput{PreviewID: lineageImport.ID,
		ManifestDigest: lineageImport.ManifestDigest, ProjectName: "Lineage import", DestinationRoot: filepath.Join(t.TempDir(), "lineage"), Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	var importedEdges int
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM artifact_derivations d JOIN artifacts result ON result.id=d.result_artifact_id
		JOIN artifacts source ON source.id=d.source_artifact_id WHERE result.project_id=? AND source.project_id=?`, importedProject.ID, importedProject.ID).Scan(&importedEdges); err != nil {
		t.Fatal(err)
	}
	if importedEdges != 1 {
		t.Fatalf("imported derivation edges=%d", importedEdges)
	}

	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	w, _ := zw.Create("../escape")
	_, _ = w.Write([]byte("bad"))
	_ = zw.Close()
	if _, err = service.PreviewShareImport(ctx, body.Bytes(), "owner"); err == nil {
		t.Fatal("path traversal archive was accepted")
	}
	body.Reset()
	zw = zip.NewWriter(&body)
	w, _ = zw.Create("manifest.json")
	_, _ = w.Write([]byte(`{"format":"hermetrix.project-share.v1","project_name":"x","entries":[]}`))
	w, _ = zw.Create("MANIFEST.JSON")
	_, _ = w.Write([]byte("duplicate"))
	_ = zw.Close()
	if _, err = service.PreviewShareImport(ctx, body.Bytes(), "owner"); err == nil {
		t.Fatal("case-colliding archive was accepted")
	}
}

func TestShareArchiveAcceptsDocumentedEntryRangeWithStreamingValidation(t *testing.T) {
	const count = 1001
	manifest := shareManifest{Format: shareFormat, PackageKind: "project_share", FormatVersion: 1,
		SourceApplication: "hermetrix-harness", SourceSchemaVersion: store.CurrentSchemaVersion,
		ExportID: "share-large-test", ProjectName: "Large valid share"}
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("file-%04d.txt", index)
		manifest.Entries = append(manifest.Entries, ShareManifestEntry{Kind: "file", Path: name, SHA256: checksum([]byte("x")), Bytes: 1})
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	zw := zip.NewWriter(&body)
	w, _ := zw.Create("manifest.json")
	_, _ = w.Write(manifestBody)
	for _, entry := range manifest.Entries {
		w, _ = zw.Create("files/" + entry.Path)
		_, _ = w.Write([]byte("x"))
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	validated, err := validateShareArchive(body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(validated.Entries) != count {
		t.Fatalf("validated entries=%d", len(validated.Entries))
	}
}

func TestProjectShareImportsTasksAsDraftsSkillsAsCandidatesAndMemoriesPrivate(t *testing.T) {
	service, skillService, dataStore := testProductService(t)
	ctx := context.Background()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Projection Source", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "project", ObjectID: project.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "share projections"}); err != nil {
		t.Fatal(err)
	}
	tasks := taskengine.NewService(dataStore)
	task, err := tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Imported work", Objective: "remain a draft",
		OriginalRequest: "carry requirements only", Constraints: []string{"local"}, Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "draft only"}}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "task", ObjectID: task.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "share draft projection"}); err != nil {
		t.Fatal(err)
	}
	memory, err := service.SaveMemory(ctx, MemoryInput{ScopeKind: "project", ScopeRef: project.ID, MemoryKind: "decision", Content: "portable decision", Source: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "memory", ObjectID: memory.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "share memory projection"}); err != nil {
		t.Fatal(err)
	}
	markdown := "---\nname: portable-skill\ndescription: \"A bounded portable procedure\"\ntags: [test]\ntools: []\n---\n\n# Procedure\n\n1. Read the explicit input.\n2. Return bounded evidence.\n"
	candidate, err := skillService.CreateCandidate(ctx, skills.CreateCandidateInput{CanonicalName: "portable-skill", ScopeKind: "project", ScopeRef: project.ID,
		Origin: "user_created", Owner: "user", ChangeKind: "create", CreatedBy: "user", TriggerKind: "manual", Reason: "share fixture", Markdown: markdown})
	if err != nil {
		t.Fatal(err)
	}
	skill, err := skillService.PromoteCandidate(ctx, candidate.ID, "owner", candidate.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UpdateVisibility(ctx, VisibilityInput{ObjectKind: "skill", ObjectID: skill.ID, Visibility: "project_shared",
		ExportPolicy: "explicit_selection", ExpectedRevision: 1, Actor: "owner", Reason: "share skill projection"}); err != nil {
		t.Fatal(err)
	}
	preview, err := service.CreateSharePreview(ctx, SharePreviewInput{ProjectID: project.ID, TaskIDs: []string{task.ID},
		SkillIDs: []string{skill.ID}, MemoryIDs: []string{memory.ID}, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 3 {
		t.Fatalf("preview entries=%+v", preview.Entries)
	}
	exported, err := service.ExportShare(ctx, ShareExportInput{ProjectID: project.ID, PreviewID: preview.ID,
		ManifestDigest: preview.ManifestDigest, IdempotencyKey: "projection-export"})
	if err != nil {
		t.Fatal(err)
	}
	_, packageBody, err := service.ShareExportContent(ctx, exported.ID)
	if err != nil {
		t.Fatal(err)
	}
	importPreview, err := service.PreviewShareImport(ctx, packageBody, "owner")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "projection-import")
	imported, err := service.ApplyShareImport(ctx, ApplyShareImportInput{PreviewID: importPreview.ID, ManifestDigest: importPreview.ManifestDigest,
		ProjectName: "Imported projections", DestinationRoot: destination, Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	var taskState, taskVisibility, taskPolicy string
	if err = dataStore.DB.QueryRow(`SELECT state,visibility,export_policy FROM durable_tasks WHERE project_id=?`, imported.ID).Scan(&taskState, &taskVisibility, &taskPolicy); err != nil {
		t.Fatal(err)
	}
	if taskState != "draft" || taskVisibility != "private" || taskPolicy != "deny" {
		t.Fatalf("imported task state/policy=%s/%s/%s", taskState, taskVisibility, taskPolicy)
	}
	var liveAuthority int
	if err = dataStore.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM task_runs r JOIN durable_tasks t ON t.id=r.task_id WHERE t.project_id=?)+
		(SELECT COUNT(*) FROM tool_approvals a JOIN agent_sessions s ON s.id=a.session_id WHERE s.project_id=?)`, imported.ID, imported.ID).Scan(&liveAuthority); err != nil {
		t.Fatal(err)
	}
	if liveAuthority != 0 {
		t.Fatalf("import restored live authority: %d", liveAuthority)
	}
	var memoryVisibility, memoryPolicy string
	if err = dataStore.DB.QueryRow(`SELECT visibility,export_policy FROM memories WHERE scope_kind='project' AND scope_ref=?`, imported.ID).Scan(&memoryVisibility, &memoryPolicy); err != nil {
		t.Fatal(err)
	}
	if memoryVisibility != "private" || memoryPolicy != "deny" {
		t.Fatalf("imported memory policy=%s/%s", memoryVisibility, memoryPolicy)
	}
	var candidateCount int
	if err = dataStore.DB.QueryRow(`SELECT COUNT(*) FROM skill_candidates WHERE scope_kind='project' AND scope_ref=? AND visibility='private' AND export_policy='deny'`, imported.ID).Scan(&candidateCount); err != nil {
		t.Fatal(err)
	}
	if candidateCount != 1 {
		t.Fatalf("imported Skill candidates=%d", candidateCount)
	}
}
