package product

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAbsentSnapshotsAndSourceManifestIncludeExplicitMQLCreation(t *testing.T) {
	s, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.mqh"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.SaveProject(ctx, ProjectInput{Name: "EA", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	absent, err := s.SnapshotProjectFile(ctx, p.ID, "EA.mq5")
	if err != nil || absent.SHA256 != "absent" {
		t.Fatalf("snapshot=%+v err=%v", absent, err)
	}
	empty, err := s.SnapshotProjectFile(ctx, p.ID, "empty.mqh")
	if err != nil || empty.SHA256 == "absent" || empty.SHA256 != hashBytes(nil) {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	files, err := s.ProjectFileManifestForWrites(ctx, p.ID, []string{"EA.mq5"}, 10)
	if err != nil || len(files) != 2 || files[0].Path != "EA.mq5" || !files[0].Absent {
		t.Fatalf("manifest=%+v err=%v", files, err)
	}
	if _, err = os.Stat(filepath.Join(root, "EA.mq5")); !os.IsNotExist(err) {
		t.Fatal("snapshot created a placeholder")
	}
	for _, name := range []string{".env.local", "credentials.json", "../outside.js", "missing/new.js"} {
		if _, err = s.ProjectFileManifestForWrites(ctx, p.ID, []string{name}, 10); err == nil {
			t.Fatalf("unsafe or unsupported creation candidate=%s", name)
		}
	}
}

func TestCreateBatchUsesAbsentPreimageAndRefusesConcurrentOverwrite(t *testing.T) {
	s, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	p, err := s.SaveProject(ctx, ProjectInput{Name: "create", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.WriteProjectFiles(ctx, p.ID, []WriteFileInput{{Path: "new.js", Content: "module.exports=42;", ExpectedSHA256: "absent", Actor: "owner"}}); err == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("concurrent creates=%d", wins.Load())
	}
	current, err := s.ReadProjectFile(ctx, p.ID, "new.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteProjectFiles(ctx, p.ID, []WriteFileInput{{Path: "new.js", Content: "overwritten", ExpectedSHA256: "absent", Actor: "owner"}}); err == nil {
		t.Fatal("absent preimage overwrote existing file")
	}
	rollback, err := s.RestoreProjectFileAbsence(ctx, p.ID, "new.js", current.SHA256, "rollback")
	if err != nil || rollback.State != "restored" || rollback.ArtifactID == "" {
		t.Fatalf("rollback=%+v err=%v", rollback, err)
	}
	if _, err = os.Stat(filepath.Join(root, "new.js")); !os.IsNotExist(err) {
		t.Fatal("rollback did not restore absence")
	}
	_, body, err := s.GetArtifact(ctx, rollback.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Quarantine string `json:"quarantine_path"`
	}
	if err = json.Unmarshal(body, &receipt); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(receipt.Quarantine)
	if err != nil || string(retained) != current.Content {
		t.Fatalf("retained bytes=%q err=%v", retained, err)
	}
}

func TestCreationRollbackPreservesLaterEditsAndRejectsMissingProof(t *testing.T) {
	s, _, db := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	p, err := s.SaveProject(ctx, ProjectInput{Name: "conflict", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.WriteProjectFile(ctx, p.ID, WriteFileInput{Path: "new.js", Content: "generated", ExpectedSHA256: "absent", Actor: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "new.js"), []byte("later edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RestoreProjectFileAbsence(ctx, p.ID, "new.js", created.Document.SHA256, "rollback"); err == nil {
		t.Fatal("rollback removed a newer edit")
	}
	current, _ := os.ReadFile(filepath.Join(root, "new.js"))
	if string(current) != "later edit" {
		t.Fatalf("later edit=%s", current)
	}
	intent, err := s.createFileMutationIntent(ctx, p.ID, "absent.js", "rollback", hashBytes([]byte("generated")), "absent")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.transitionFileMutation(ctx, intent.OperationID, mutationPlanned, mutationDispatched, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecoverFileMutations(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = db.DB.QueryRow(`SELECT state FROM file_mutation_intents WHERE operation_id=?`, intent.OperationID).Scan(&state); err != nil || state != mutationUncertain {
		t.Fatalf("unproved absence state=%s err=%v", state, err)
	}
}

func TestBatchCreateRollsBackAllFilesAfterReceiptFailure(t *testing.T) {
	s, _, db := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	p, err := s.SaveProject(ctx, ProjectInput{Name: "batch", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`CREATE TRIGGER fail_created_receipt BEFORE INSERT ON artifacts WHEN NEW.name='z.js.diff' AND json_extract(NEW.metadata_json,'$.actor')='batch-create' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.WriteProjectFiles(ctx, p.ID, []WriteFileInput{{Path: "a.js", Content: "a", ExpectedSHA256: "absent", Actor: "batch-create"}, {Path: "z.js", Content: "z", ExpectedSHA256: "absent", Actor: "batch-create"}})
	var committed *MutationCommittedReceiptFailed
	if !errors.As(err, &committed) || len(result.Receipts) != 2 || len(result.Rollback) != 2 {
		t.Fatalf("batch=%+v err=%v", result, err)
	}
	for _, name := range []string{"a.js", "z.js"} {
		if _, err = os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("file remained after failed batch: %s", name)
		}
	}
	for _, r := range result.Rollback {
		if r.State != "restored" || r.ArtifactID == "" {
			t.Fatalf("rollback lacks receipt: %+v", r)
		}
	}
}

func TestRootGuardPreventsRedirectedBatchAndCancellableRootChange(t *testing.T) {
	s, _, _ := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	p, err := s.SaveProject(ctx, ProjectInput{Name: "root guard", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := s.ProjectRootIdentity(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := s.guardProjectRoot(ctx, p.ID, frozen)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if release, guardErr := s.guardProjectRoot(cancelCtx, p.ID, frozen); !errors.Is(guardErr, context.Canceled) {
		if release != nil {
			release()
		}
		unlock()
		t.Fatalf("cancelled lock acquisition=%v", guardErr)
	}
	if _, err = s.SaveProject(cancelCtx, ProjectInput{ID: p.ID, Name: "moved", RootPath: other}); !errors.Is(err, context.Canceled) {
		unlock()
		t.Fatalf("root change under held guard=%v", err)
	}
	unlock()
	if current, err := s.ProjectRootIdentity(ctx, p.ID); err != nil || current != frozen {
		t.Fatalf("cancelled root change committed: root=%s err=%v", current, err)
	}
	if _, err = s.SaveProject(ctx, ProjectInput{ID: p.ID, Name: "moved", RootPath: other}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteProjectFilesAtRoot(ctx, p.ID, frozen, []WriteFileInput{{Path: "new.js", Content: "new", ExpectedSHA256: "absent", Actor: "owner"}}); err == nil {
		t.Fatal("write followed a changed project root")
	}
	if _, err = s.SnapshotProjectFileAtRoot(ctx, p.ID, frozen, "new.js"); err == nil {
		t.Fatal("snapshot read a changed project root")
	}
	if _, err = s.StartCommand(ctx, CommandInput{ProjectID: p.ID, ExpectedRoot: frozen, Actor: "owner", Executable: "node", Arguments: []string{"-e", "require('fs').writeFileSync('new.js','wrong root')"}}); err == nil {
		t.Fatal("command accepted a changed project root")
	}
	for _, dir := range []string{root, other} {
		if _, err = os.Stat(filepath.Join(dir, "new.js")); !os.IsNotExist(err) {
			t.Fatalf("write escaped frozen scope: %s", dir)
		}
	}
}

func TestAbsenceRecoveryRequiresIntactBoundQuarantineReceipt(t *testing.T) {
	for _, mode := range []string{"intact", "changed-quarantine", "wrong-artifact-kind", "changed-root"} {
		t.Run(mode, func(t *testing.T) {
			s, _, db := testProductService(t)
			ctx := context.Background()
			root := t.TempDir()
			p, err := s.SaveProject(ctx, ProjectInput{Name: "recovery", RootPath: root})
			if err != nil {
				t.Fatal(err)
			}
			created, err := s.WriteProjectFile(ctx, p.ID, WriteFileInput{Path: "new.js", Content: "generated", ExpectedSHA256: "absent", Actor: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.RestoreProjectFileAbsence(ctx, p.ID, "new.js", created.Document.SHA256, "owner")
			if err != nil {
				t.Fatal(err)
			}
			artifact, body, err := s.GetArtifact(ctx, r.ArtifactID)
			if err != nil {
				t.Fatal(err)
			}
			var receipt fileAbsenceReceipt
			if err = json.Unmarshal(body, &receipt); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "changed-quarantine":
				if err = os.WriteFile(receipt.QuarantinePath, []byte("owner edited retained evidence"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-artifact-kind":
				if _, err = db.DB.Exec(`UPDATE artifacts SET kind='unrelated' WHERE id=?`, artifact.ID); err != nil {
					t.Fatal(err)
				}
			case "changed-root":
				if _, err = s.SaveProject(ctx, ProjectInput{ID: p.ID, Name: "moved", RootPath: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate a crash after evidence persistence, before intent acknowledgement.
			if _, err = db.DB.Exec(`UPDATE file_mutation_intents SET state='dispatched',receipt_artifact_id=NULL WHERE operation_id=?`, r.OperationID); err != nil {
				t.Fatal(err)
			}
			if _, err = s.RecoverFileMutations(ctx); err != nil {
				t.Fatal(err)
			}
			want := mutationUncertain
			if mode == "intact" {
				want = mutationReconciled
			}
			var state string
			if err = db.DB.QueryRow(`SELECT state FROM file_mutation_intents WHERE operation_id=?`, r.OperationID).Scan(&state); err != nil || state != want {
				t.Fatalf("recovery=%s want=%s err=%v", state, want, err)
			}
		})
	}
}
