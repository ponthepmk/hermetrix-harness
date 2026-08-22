package context

import (
	stdcontext "context"
	"strings"
	"testing"
)

type hallucinatingCompactor struct{}

func (hallucinatingCompactor) Compact(_ stdcontext.Context, _ []Fragment, _ int, _ Estimator) (Fragment, error) {
	return Fragment{ID: "bad", Kind: KindCheckpoint, Content: "The task succeeded without evidence.", Provenance: "model"}, nil
}

func TestVerifiedCompactorRejectsUnsupportedSummaryAndFallsBack(t *testing.T) {
	compactor := NewVerifiedCompactor(hallucinatingCompactor{})
	result, err := compactor.Compact(stdcontext.Background(), []Fragment{{ID: "decision-1", Kind: KindDecision,
		Content: "Use atomic writes", Priority: 80}}, 200, NewAdaptiveEstimator())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "[decision:decision-1]") || !strings.Contains(result.Provenance, "verified-fallback") ||
		strings.Contains(result.Content, "succeeded without evidence") {
		t.Fatalf("fallback checkpoint = %+v", result)
	}
}
