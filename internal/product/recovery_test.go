package product

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"hermetrix-harness/internal/store"
)

func TestFullRecoverySnapshotsSQLiteAndCASWithoutVault(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	root := t.TempDir()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Recovery Source", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := service.CreateArtifact(ctx, ArtifactInput{ProjectID: project.ID, Name: "proof.txt", Kind: "proof", MIMEType: "text/plain", Content: "recovery evidence"})
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO browser_tabs(id,project_id,url,title,state,created_at,updated_at)
		VALUES('recovery-tab',?,'https://example.test/','Recovery page','interrupted','2026-09-26T00:00:00Z','2026-09-26T00:00:00Z')`, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dataStore.DB.ExecContext(ctx, `INSERT INTO browser_action_receipts(id,owner_principal_id,task_id,tab_id,action,
		element_ref,target_json,text_hash,observation_hash,subject_hash,screenshot_before_id,state,created_at,expires_at)
		VALUES('recovery-action',?,'','recovery-tab','click',1,'{}','text-hash','observation-hash','subject-hash',?,
		'uncertain','2026-09-26T00:00:00Z','2026-09-26T00:02:00Z')`, ownerID, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, packageBody, err := service.CreateFullRecovery(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "completed" || len(packageBody) == 0 {
		t.Fatalf("run=%+v", run)
	}
	report, err := VerifyFullRecovery(packageBody)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Compatible || report.IntegrityCheck != "ok" || report.ForeignKeyErrors != 0 || report.VerifiedBlobCount < 1 {
		t.Fatalf("report=%+v", report)
	}
	if report.Manifest.VaultIncluded {
		t.Fatal("vault was included in DB/CAS package")
	}
	_, secondPackage, err := service.CreateFullRecovery(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	secondReport, err := VerifyFullRecovery(secondPackage)
	if err != nil {
		t.Fatal(err)
	}
	if secondReport.VerifiedBlobCount != report.VerifiedBlobCount || len(secondPackage) > len(packageBody)+(4<<10) {
		t.Fatalf("successive recovery recursively embedded the prior package: first=%d/%d second=%d/%d",
			len(packageBody), report.VerifiedBlobCount, len(secondPackage), secondReport.VerifiedBlobCount)
	}
	restoreRoot := filepath.Join(t.TempDir(), "restored-data")
	if _, err = RestoreFullRecoveryTo(packageBody, restoreRoot); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(ctx, restoreRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var blobRef string
	if err = restored.DB.QueryRow(`SELECT blob_ref FROM artifacts WHERE id=?`, artifact.ID).Scan(&blobRef); err != nil {
		t.Fatal(err)
	}
	var actionState, screenshotID string
	if err = restored.DB.QueryRow(`SELECT state,screenshot_before_id FROM browser_action_receipts WHERE id='recovery-action'`).
		Scan(&actionState, &screenshotID); err != nil || actionState != "uncertain" || screenshotID != artifact.ID {
		t.Fatalf("restored browser receipt = %q %q err=%v", actionState, screenshotID, err)
	}
	body, err := restored.Blobs.Get(blobRef)
	if err != nil || string(body) != "recovery evidence" {
		t.Fatalf("restored body=%q err=%v", body, err)
	}
	if _, err = os.Stat(filepath.Join(restoreRoot, "secrets.json")); !os.IsNotExist(err) {
		t.Fatalf("vault unexpectedly restored: %v", err)
	}
}

func TestFullRecoveryRejectsTamperAndSeparatesProtectedVault(t *testing.T) {
	service, _, dataStore := testProductService(t)
	ctx := context.Background()
	vaultBody := []byte(`{"format":2,"values":{}}`)
	if err := os.WriteFile(filepath.Join(dataStore.Root, "secrets.json"), vaultBody, 0600); err != nil {
		t.Fatal(err)
	}
	capture, err := service.CaptureProtectedVault()
	if err != nil {
		t.Fatal(err)
	}
	if !capture.SameMachineOnly || capture.SHA256 == "" || string(capture.Data) != string(vaultBody) {
		t.Fatalf("capture=%+v", capture)
	}
	root := t.TempDir()
	project, err := service.SaveProject(ctx, ProjectInput{Name: "Recovery Tamper", RootPath: root})
	if err != nil {
		t.Fatal(err)
	}
	_ = project
	_, packageBody, err := service.CreateFullRecovery(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), packageBody...)
	tampered[len(tampered)/2] ^= 0xff
	if _, err = VerifyFullRecovery(tampered); err == nil {
		t.Fatal("tampered recovery package was accepted")
	}
}
