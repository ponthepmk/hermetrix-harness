package taskcoord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/taskengine"
	"hermetrix-harness/internal/worker"
)

func TestVerifyRejectsWiderScopeLegacyProposal(t *testing.T) {
	c, _, authority, attemptID, packet, providerID, root := setupOutputScope(t,
		taskengine.GoalContract{WritablePaths: []string{"sum.js", "test.js"}}, []string{"sum.js"})
	ctx := context.Background()

	doc, err := c.products.SnapshotProjectFileAtRoot(ctx, packet.ProjectID, root, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	originalSHA := doc.SHA256

	originalData, err := os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatal(err)
	}
	changedContent := string(originalData) + "// legacy changed context\n"

	if err := os.WriteFile(filepath.Join(root, "test.js"), []byte(changedContent), 0o644); err != nil {
		t.Fatal(err)
	}
	correctedSum := "function sum(a,b){return a+b;}\nmodule.exports=sum;\n"
	if err := os.WriteFile(filepath.Join(root, "sum.js"), []byte(correctedSum), 0o644); err != nil {
		t.Fatal(err)
	}

	result := worker.Result{
		ProposalID: "scope-verify-fixture",
		Changes: []worker.Change{
			{Path: "test.js", BeforeSHA256: originalSHA, Content: changedContent},
		},
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	artifact, err := c.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: packet.ProjectID,
		Name:      "scope-verify-fixture.json",
		Kind:      "code_proposal",
		MIMEType:  "application/json",
		Content:   string(body),
		Metadata:  map[string]any{"project_root": root},
	})
	if err != nil {
		t.Fatal(err)
	}

	profile, err := c.providers.Get(ctx, providerID)
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(body)
	resultHash := hex.EncodeToString(sum[:])

	proposal, err := c.tasks.RecordCodeProposal(ctx, taskengine.RecordCodeProposalInput{
		ID:               "scope-verify-fixture",
		TaskID:           packet.TaskID,
		StepID:           packet.Step.ID,
		AttemptID:        attemptID,
		ProjectID:        packet.ProjectID,
		PacketHash:       packet.CanonicalPacketHash,
		ProviderID:       providerID,
		ProviderRevision: profile.Revision,
		ArtifactID:       artifact.ID,
		ResultHash:       resultHash,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = c.tasks.DecideCodeProposal(ctx, proposal.ID, "reviewer", taskengine.ProposalApproved, "scope verify", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = c.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApproved, taskengine.ProposalApplying); err != nil {
		t.Fatal(err)
	}
	if _, err = c.tasks.TransitionCodeProposal(ctx, proposal.ID, taskengine.ProposalApplying, taskengine.ProposalApplied); err != nil {
		t.Fatal(err)
	}

	out, err := c.Verify(ctx, authority, proposal.ID, "owner", []CommandCheck{{
		ID:             "node test.js",
		Executable:     "node",
		Arguments:      []string{"test.js"},
		WorkingDir:     ".",
		TimeoutSeconds: 10,
		RequirementIDs: []string{"AC-1"},
	}})
	if err == nil {
		t.Fatal("Verify must reject wider-scope legacy proposal")
	}
	if !strings.Contains(err.Error(), "scope") && !strings.Contains(err.Error(), "writable") {
		t.Fatalf("error %q lacks scope/writable fence", err.Error())
	}
	for _, effect := range out.Effects {
		if effect.Action == "workspace.run" {
			t.Fatal("rejection must occur before any workspace.run effect")
		}
	}
}
