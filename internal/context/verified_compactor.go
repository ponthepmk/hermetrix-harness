package context

import (
	stdcontext "context"
	"fmt"
	"strings"
)

// VerifiedCompactor keeps semantic compaction behind a deterministic evidence
// boundary. A primary compactor may be model-backed, but its checkpoint is
// accepted only when every statement carries a known source marker and causal
// pairs are represented together. Otherwise the extractive fallback is used.
type VerifiedCompactor struct {
	primary  Compactor
	fallback Compactor
}

func NewVerifiedCompactor(primary Compactor) VerifiedCompactor {
	if primary == nil {
		primary = StructuredCompactor{}
	}
	return VerifiedCompactor{primary: primary, fallback: StructuredCompactor{}}
}

func (c VerifiedCompactor) Compact(ctx stdcontext.Context, fragments []Fragment, targetTokens int, estimator Estimator) (Fragment, error) {
	checkpoint, err := c.primary.Compact(ctx, fragments, targetTokens, estimator)
	if err == nil && verifyCheckpointEvidence(checkpoint, fragments) == nil {
		checkpoint.Provenance += ":verified"
		return checkpoint, nil
	}
	fallback, fallbackErr := c.fallback.Compact(ctx, fragments, targetTokens, estimator)
	if fallbackErr != nil {
		return Fragment{}, fmt.Errorf("primary compaction invalid (%v); fallback failed: %w", err, fallbackErr)
	}
	if verifyErr := verifyCheckpointEvidence(fallback, fragments); verifyErr != nil {
		return Fragment{}, fmt.Errorf("deterministic fallback violated evidence contract: %w", verifyErr)
	}
	fallback.Provenance += ":verified-fallback"
	return fallback, nil
}

func verifyCheckpointEvidence(checkpoint Fragment, source []Fragment) error {
	if checkpoint.Content == "" {
		return nil
	}
	known := map[string]bool{}
	pairs := map[string][]string{}
	for _, fragment := range source {
		marker := fmt.Sprintf("[%s:%s]", fragment.Kind, fragment.ID)
		known[marker] = true
		if fragment.PairID != "" {
			pairs[fragment.PairID] = append(pairs[fragment.PairID], marker)
		}
	}
	for _, line := range strings.Split(checkpoint.Content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || isCheckpointPreamble(line) {
			continue
		}
		if !strings.HasPrefix(line, "- [") {
			return fmt.Errorf("checkpoint statement has no evidence marker")
		}
		end := strings.Index(line, "]")
		if end < 0 || !known[line[2:end+1]] {
			return fmt.Errorf("checkpoint references an unknown source")
		}
	}
	for pairID, markers := range pairs {
		present := 0
		for _, marker := range markers {
			if strings.Contains(checkpoint.Content, marker) {
				present++
			}
		}
		if present != 0 && present != len(markers) {
			return fmt.Errorf("checkpoint split causal pair %s", pairID)
		}
	}
	return nil
}
