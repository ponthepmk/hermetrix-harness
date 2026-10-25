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

func TestApplyRefusesFutureOutputPath(t *testing.T) {
	c, _, authority, attemptID, packet, providerID, root := setupOutputScope(t,
		taskengine.GoalContract{WritablePaths: []string{"sum.js", "test.js"}}, []string{"sum.js"})
	ctx := context.Background()

	doc, err := c.products.SnapshotProjectFileAtRoot(ctx, packet.ProjectID, root, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	observedSHA := doc.SHA256

	result := worker.Result{
		ProposalID: "scope-apply-fixture",
		Changes: []worker.Change{
			{Path: "test.js", BeforeSHA256: observedSHA, Content: "changed by fixture\n"},
		},
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	artifact, err := c.products.CreateArtifact(ctx, product.ArtifactInput{
		ProjectID: packet.ProjectID,
		Name:      "scope-apply-fixture.json",
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
		ID:               "scope-apply-fixture",
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

	if _, err = c.tasks.DecideCodeProposal(ctx, proposal.ID, "reviewer", taskengine.ProposalApproved, "scope test", nil); err != nil {
		t.Fatal(err)
	}

	out, err := c.Apply(ctx, authority, proposal.ID, "owner")
	if err == nil {
		t.Fatal("Apply must refuse out-of-scope path")
	}
	if !strings.Contains(err.Error(), "writable") && !strings.Contains(err.Error(), "scope") {
		t.Fatalf("error %q lacks writable/scope fence", err.Error())
	}

	data, err := os.ReadFile(filepath.Join(root, "test.js"))
	if err != nil {
		t.Fatal(err)
	}
	want := "const assert=require('node:assert/strict');assert.equal(require('./sum')(2,3),5);assert.equal(require('./sum')(-4,7),3);console.log('acceptance passed');\n"
	if string(data) != want {
		t.Fatal("test.js was modified")
	}

	if len(out.Receipts) != 0 {
		t.Fatalf("got %d receipts, want 0", len(out.Receipts))
	}

	p, err := c.tasks.GetCodeProposal(ctx, proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != taskengine.ProposalApproved {
		t.Fatalf("state=%s want %s", p.State, taskengine.ProposalApproved)
	}
}
