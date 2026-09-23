package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ctxcompiler "hermetrix-harness/internal/context"
)

func TestProjectBrainEvidenceFlowsThroughCompilerAsUntrustedReference(t *testing.T) {
	service, provider, cleanup := testAgentService(t, httptest.NewServer(http.NotFoundHandler()))
	defer cleanup()
	ctx := context.Background()
	session, err := service.CreateSession(ctx, CreateSessionInput{Title: "knowledge", ProviderID: provider.ID,
		ContextProfile: "certified-64k", QualificationOverride: testQualificationOverride()})
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := ctxcompiler.ProfileByName("certified-64k")
	knowledge := ctxcompiler.Fragment{ID: "project-brain:verified", Kind: ctxcompiler.KindProjectKnowledge,
		Scope: "project_brain:client-a", Provenance: "pi-second-brain", Trust: "external_curated_not_verified",
		Version: "sha256:" + strings.Repeat("a", 64), Priority: 58, Content: "Reference only: state DB recovery",
		CreatedAt: time.Now().UTC()}
	compiled, _, err := service.compileTurnWithKnowledge(ctx, profile, []Event{{ID: "goal", SessionID: session.ID,
		TurnID: "t1", EventKind: "message", Role: "user", Content: "recover state database",
		CreatedAt: time.Now().UTC()}}, "t1", session.Contract,
		ctxcompiler.ScriptEstimator{NonASCIIRate: 0.55, Scale: 1}, TransportOverhead{}, []ctxcompiler.Fragment{knowledge})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.DirectTools) != len(session.Contract.ToolBindings) || compiled.Report.UnaccountedTokens != 0 {
		t.Fatalf("knowledge changed tool authority or broke the context ledger: %+v", compiled.Report)
	}
	selected := false
	for _, fragment := range compiled.Fragments {
		if fragment.ID == knowledge.ID {
			selected = fragment.Kind == ctxcompiler.KindProjectKnowledge && fragment.Trust == knowledge.Trust && !fragment.Pinned
		}
	}
	if !selected || !strings.Contains(renderMessages(compiled.Fragments)[0].Content, "Reference only: state DB recovery") {
		t.Fatalf("Project Brain evidence did not reach the actual provider context")
	}
}
