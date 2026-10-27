package taskcoord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"hermetrix-harness/internal/worker"
)

const maxProposalBodyBytes = 16 << 20

// proposalBundle is the private stored shape of a proposal.
// Version 2 is the current envelope; Version 1 is the legacy raw result.
type proposalBundle struct {
	Version         int           `json:"version"`
	InputArtifactID string        `json:"input_artifact_id"`
	Result          worker.Result `json:"result"`
}

// decodeProposalBundle decodes a stored proposal body into a proposalBundle.
// It accepts either a strict v2 envelope or a strict raw v1 result. It returns
// a zero proposalBundle and an error on any failure.
func decodeProposalBundle(body []byte) (out proposalBundle, err error) {
	defer func() {
		if err != nil {
			out = proposalBundle{}
		}
	}()
	if len(body) == 0 || len(body) > maxProposalBodyBytes {
		return out, errors.New("proposal body must be nonempty and at most 16MiB")
	}

	// Probe for a version field without committing to a decode path.
	var probe struct {
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return out, fmt.Errorf("proposal body is not valid JSON: %w", err)
	}

	if probe.Version != nil {
		// Version present: require exactly 2 and decode the strict envelope.
		var version int
		if err := json.Unmarshal(probe.Version, &version); err != nil {
			return out, fmt.Errorf("version must be an integer: %w", err)
		}
		if version != 2 {
			return out, fmt.Errorf("unsupported proposal version %d", version)
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&out); err != nil {
			return out, fmt.Errorf("failed to decode v2 envelope: %w", err)
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			return out, fmt.Errorf("v2 envelope must contain exactly one JSON value: %w", err)
		}
		if out.Version != 2 {
			return out, fmt.Errorf("version mismatch: expected 2, got %d", out.Version)
		}
		if strings.TrimSpace(out.InputArtifactID) == "" {
			return out, errors.New("input_artifact_id must be nonblank")
		}
		if err := validateResult(out.Result); err != nil {
			return out, fmt.Errorf("v2 result invalid: %w", err)
		}
		return out, nil
	}

	// Version absent: decode the strict raw legacy result.
	var legacy worker.Result
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&legacy); err != nil {
		return out, fmt.Errorf("failed to decode legacy result: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return out, fmt.Errorf("legacy result must contain exactly one JSON value: %w", err)
	}
	if err := validateLegacyResult(legacy); err != nil {
		return out, fmt.Errorf("legacy result invalid: %w", err)
	}
	out = proposalBundle{Version: 1, Result: legacy}
	return out, nil
}

func validateResult(r worker.Result) error {
	if r.Status != "proposed_unverified" {
		return fmt.Errorf("status must be proposed_unverified, got %q", r.Status)
	}
	if strings.TrimSpace(r.ProposalID) == "" {
		return errors.New("proposal_id must be nonempty")
	}
	if len(r.Changes) < 1 || len(r.Changes) > 32 {
		return fmt.Errorf("changes must contain 1..32 entries, got %d", len(r.Changes))
	}
	return nil
}

func validateLegacyResult(r worker.Result) error {
	if err := validateResult(r); err != nil {
		return err
	}
	for i, c := range r.Changes {
		if len(c.Edits) > 0 {
			return fmt.Errorf("legacy result changes[%d] must not carry edits", i)
		}
	}
	return nil
}
