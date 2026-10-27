package worker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareWorkerTaskIndependentSnapshot(t *testing.T) {
	prefix := "PRIVATE_ONLY_MARKER|" + strings.Repeat("h", 140000)
	visible := "return a-b"
	suffix := "|HIDDEN_END"
	bigSource := prefix + visible + suffix
	fullSHA := Hash(bigSource)

	view, err := NewBoundedFileView("big.go", bigSource, fullSHA, len(prefix), len(prefix)+len(visible))
	if err != nil {
		t.Fatalf("NewBoundedFileView: %v", err)
	}

	task := Task{
		ID:                 "H1D-A3-independent-snapshot-wire-test-20261026",
		Kind:               "frozen_snapshot_acceptance",
		Brief:              "Write ONE small Go test file in package worker for prepareWorkerTask.",
		AcceptanceCriteria: []string{"Large full source preserved in canonical but absent from wire"},
		Constraints:        []string{"Only new snapshot test"},
		RecommendedChecks:  []string{"go test ./internal/worker"},
		ProjectBrainRefs: []KnowledgeRef{{
			Citation:   "project-brain:curated/fix.md:1",
			Version:    "sha256:" + strings.Repeat("a", 64),
			Provenance: "pi-second-brain",
			Trust:      "external_curated_not_verified",
			Content:    "reference",
		}},
		Files: map[string]string{
			"big.go":      bigSource,
			"context.txt": "public context",
			"new.go":      "",
		},
		FileViews: map[string][]BoundedFileView{
			"big.go": {view},
		},
		WritablePaths: []string{"big.go", "new.go"},
		AbsentFiles:   []string{"new.go"},
		FailureEvidence: []FailureEvidence{{
			ValidationID:    "v1",
			CheckID:         "c1",
			SubjectRevision: "r1",
			Actual:          "actual",
			ArtifactID:      "a1",
			ArtifactSHA256:  "s1",
			Output:          "output",
			Truncated:       false,
		}},
		MaxOutputTokens: 3072,
	}

	expectedCanonical, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal original task: %v", err)
	}

	snapshot, canonical, wire, err := prepareWorkerTask(task)
	if err != nil {
		t.Fatalf("prepareWorkerTask: %v", err)
	}
	if canonical == nil || len(canonical) <= 128*1024 {
		t.Fatalf("canonical length %d", len(canonical))
	}
	if string(canonical) != string(expectedCanonical) {
		t.Fatalf("canonical differs from original marshal")
	}
	if snapshot.Files["big.go"] != bigSource {
		t.Fatalf("snapshot big.go differs")
	}

	var wireTask Task
	if err := json.Unmarshal(wire, &wireTask); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	if _, ok := wireTask.Files["big.go"]; ok {
		t.Fatalf("big.go present in wire Files")
	}
	if wireTask.Files["context.txt"] != "public context" || wireTask.Files["new.go"] != "" {
		t.Fatalf("context.txt/new.go not retained")
	}
	if len(wireTask.FileViews["big.go"]) != 1 || wireTask.FileViews["big.go"][0].Content != visible || wireTask.FileViews["big.go"][0].FullSHA256 != fullSHA {
		t.Fatalf("wire FileViews big.go incorrect")
	}
	if strings.Contains(string(wire), "PRIVATE_ONLY_MARKER") || strings.Contains(string(wire), "HIDDEN_END") {
		t.Fatalf("wire contains hidden content")
	}
	if len(wire) > 128*1024 {
		t.Fatalf("wire length %d", len(wire))
	}
	if Hash(string(canonical)) == Hash(string(wire)) {
		t.Fatalf("canonical and wire hashes equal")
	}

	// Mutate original task to detect aliases.
	task.Files["big.go"] = "mutated"
	task.Files["context.txt"] = "mutated context"
	task.Files["new.go"] = "mutated new"
	task.FileViews["big.go"][0].Content = "mutated view"
	task.FileViews["big.go"][0].FullSHA256 = "mutated sha"
	task.FileViews["big.go"][0].Path = "mutated path"
	task.FileViews["big.go"][0].Start = 1
	task.FileViews["big.go"][0].End = 2
	task.FileViews["big.go"][0].RegionID = "mutated region"
	task.AcceptanceCriteria[0] = "mutated acceptance"
	task.Constraints[0] = "mutated constraint"
	task.RecommendedChecks[0] = "mutated check"
	task.ProjectBrainRefs[0].Citation = "mutated citation"
	task.ProjectBrainRefs[0].Version = "mutated version"
	task.ProjectBrainRefs[0].Provenance = "mutated provenance"
	task.ProjectBrainRefs[0].Trust = "mutated trust"
	task.ProjectBrainRefs[0].Content = "mutated content"
	task.WritablePaths[0] = "mutated writable"
	task.WritablePaths[1] = "mutated writable2"
	task.AbsentFiles[0] = "mutated absent"
	task.FailureEvidence[0].ValidationID = "mutated validation"
	task.FailureEvidence[0].CheckID = "mutated check"
	task.FailureEvidence[0].SubjectRevision = "mutated revision"
	task.FailureEvidence[0].Actual = "mutated actual"
	task.FailureEvidence[0].ArtifactID = "mutated artifact"
	task.FailureEvidence[0].ArtifactSHA256 = "mutated artifact sha"
	task.FailureEvidence[0].Output = "mutated output"
	task.FailureEvidence[0].Truncated = true
	task.MaxOutputTokens = 999
	task.ID = "mutated id"
	task.Kind = "mutated kind"
	task.Brief = "mutated brief"

	reCanonical, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot after mutation: %v", err)
	}
	if string(reCanonical) != string(expectedCanonical) {
		t.Fatalf("snapshot canonical changed after original mutation")
	}

}
