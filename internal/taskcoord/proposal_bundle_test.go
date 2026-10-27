package taskcoord

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type frozenCase struct {
	Name            string `json:"name"`
	Body            string `json:"body"`
	WantError       bool   `json:"want_error"`
	ExpectedVersion int    `json:"expected_version"`
	ExpectedInput   string `json:"expected_input"`
	ExpectedChanges int    `json:"expected_changes"`
}

func TestDecodeProposalBundleFrozenCases(t *testing.T) {
	data, err := os.ReadFile("proposal_bundle_fixture.json")
	if err != nil {
		t.Errorf("read fixture: %v", err)
		return
	}
	var cases []frozenCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Errorf("decode fixture: %v", err)
		return
	}
	if len(cases) != 29 {
		t.Errorf("expected 29 cases, got %d", len(cases))
		return
	}
	for _, c := range cases {
		actual, err := decodeProposalBundle([]byte(c.Body))
		if (err != nil) != c.WantError {
			t.Errorf("case %q: error presence mismatch: got %v, want %v", c.Name, err != nil, c.WantError)
			continue
		}
		if c.WantError {
			if !reflect.DeepEqual(actual, proposalBundle{}) {
				t.Errorf("case %q: expected zero proposalBundle on error, got %+v", c.Name, actual)
			}
			continue
		}
		if actual.Version != c.ExpectedVersion {
			t.Errorf("case %q: expected version %d, got %d", c.Name, c.ExpectedVersion, actual.Version)
		}
		if actual.InputArtifactID != c.ExpectedInput {
			t.Errorf("case %q: expected input %q, got %q", c.Name, c.ExpectedInput, actual.InputArtifactID)
		}
		if len(actual.Result.Changes) != c.ExpectedChanges {
			t.Errorf("case %q: expected %d changes, got %d", c.Name, c.ExpectedChanges, len(actual.Result.Changes))
		}
		if actual.Result.Status != "proposed_unverified" {
			t.Errorf("case %q: expected status proposed_unverified, got %q", c.Name, actual.Result.Status)
		}
	}
}
