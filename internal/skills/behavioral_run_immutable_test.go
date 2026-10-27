package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBehavioralRunStoredManifestImmutable(t *testing.T) {
	service, dataStore := testService(t)
	ctx := context.Background()
	candidate, err := service.CreateCandidate(ctx, candidateInput("run-storage", "Run storage fixture", "# Procedure\n\n1. Check storage."))
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	owner, err := dataStore.OwnerPrincipalID(ctx)
	if err != nil {
		t.Fatalf("owner principal: %v", err)
	}
	data, err := os.ReadFile("behavioral_run_manifest_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_run_manifest_cases.json: %v", err)
	}
	var root struct {
		Cases []behavioralRunManifestCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse behavioral_run_manifest_cases.json: %v", err)
	}
	var manifest BehavioralRunManifest
	found := false
	for _, c := range root.Cases {
		if c.ExpectedValid {
			manifest = c.Manifest
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no valid manifest case found")
	}
	manifest.CandidateID = candidate.ID
	manifest.CandidateRevision = candidate.Revision
	manifest.CandidateHash = candidate.CandidateHash
	manifest.CandidateBlobRef = candidate.CandidateBlobRef
	manifest.OwnerPrincipalID = owner
	manifest.ScopeKind = candidate.ScopeKind
	manifest.ScopeRef = candidate.ScopeRef
	encoded, hash, err := behavioralManifestJSON(manifest)
	if err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	sum := sha256.Sum256(encoded)
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("manifest hash mismatch")
	}
	id := "fixture-run"
	startedAt := formatTime(time.Now().UTC())
	rows, err := dataStore.DB.Exec(`INSERT INTO behavioral_eval_runs (id, owner_principal_id, candidate_id, candidate_revision, candidate_hash, manifest_json, manifest_sha256, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, owner, candidate.ID, candidate.Revision, candidate.CandidateHash, string(encoded), hash, startedAt)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
	rowsAffected, err := rows.RowsAffected()
	if err != nil || rowsAffected != 1 {
		t.Fatalf("expected 1 row inserted, got %d: %v", rowsAffected, err)
	}
	var state string
	if err := dataStore.DB.QueryRow(`SELECT state FROM behavioral_eval_runs WHERE id = ?`, id).Scan(&state); err != nil {
		t.Fatalf("select state: %v", err)
	}
	if state != BehavioralRunPrepared {
		t.Fatalf("expected prepared state, got %q", state)
	}
	for _, col := range []string{"id", "owner_principal_id", "candidate_id", "candidate_revision", "candidate_hash", "manifest_json", "manifest_sha256", "started_at"} {
		_, err := dataStore.DB.Exec(`UPDATE behavioral_eval_runs SET `+col+` = `+col+` WHERE id = ?`, id)
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("update %s should fail with immutable error, got %v", col, err)
		}
	}
	_, err = dataStore.DB.Exec(`UPDATE behavioral_eval_runs SET state = 'unknown' WHERE id = ?`, id)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("invalid state update should fail with CHECK error, got %v", err)
	}
	rows, err = dataStore.DB.Exec(`UPDATE behavioral_eval_runs SET state = ? WHERE id = ? AND state = ?`, BehavioralRunDispatched, id, BehavioralRunPrepared)
	if err != nil {
		t.Fatalf("CAS dispatch: %v", err)
	}
	rowsAffected, err = rows.RowsAffected()
	if err != nil || rowsAffected != 1 {
		t.Fatalf("expected 1 row dispatched, got %d: %v", rowsAffected, err)
	}
	rows, err = dataStore.DB.Exec(`UPDATE behavioral_eval_runs SET state = ? WHERE id = ? AND state = ?`, BehavioralRunDispatched, id, BehavioralRunPrepared)
	if err != nil {
		t.Fatalf("repeat CAS dispatch: %v", err)
	}
	rowsAffected, err = rows.RowsAffected()
	if err != nil || rowsAffected != 0 {
		t.Fatalf("expected 0 rows on repeat CAS, got %d: %v", rowsAffected, err)
	}
	var manifestJSON, manifestSHA256, state2 string
	if err := dataStore.DB.QueryRow(`SELECT manifest_json, manifest_sha256, state FROM behavioral_eval_runs WHERE id = ?`, id).Scan(&manifestJSON, &manifestSHA256, &state2); err != nil {
		t.Fatalf("select final: %v", err)
	}
	if manifestJSON != string(encoded) {
		t.Fatalf("manifest_json mutated")
	}
	if manifestSHA256 != hash {
		t.Fatalf("manifest_sha256 mutated")
	}
	if state2 != BehavioralRunDispatched {
		t.Fatalf("expected dispatched, got %q", state2)
	}
}
