package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type behavioralRunManifestCase struct {
	Name          string                `json:"name"`
	ExpectedValid bool                  `json:"expected_valid"`
	Manifest      BehavioralRunManifest `json:"manifest"`
}

type behavioralRunBindingCases struct {
	OversizeBindings BehavioralRunBindings `json:"oversize_bindings"`
}

func TestBehavioralRunManifestMatrix(t *testing.T) {
	data, err := os.ReadFile("behavioral_run_manifest_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_run_manifest_cases.json: %v", err)
	}
	var manifestRoot struct {
		Cases []behavioralRunManifestCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifestRoot); err != nil {
		t.Fatalf("parse behavioral_run_manifest_cases.json: %v", err)
	}
	cases := manifestRoot.Cases
	if len(cases) != 25 {
		t.Fatalf("expected 25 manifest cases, got %d", len(cases))
	}
	expectedValid := 0
	expectedInvalid := 0
	for _, c := range cases {
		if c.ExpectedValid {
			expectedValid++
		} else {
			expectedInvalid++
		}
	}
	if expectedValid != 5 || expectedInvalid != 20 {
		t.Fatalf("expected 5 valid and 20 invalid cases, got %d valid and %d invalid", expectedValid, expectedInvalid)
	}

	for _, c := range cases {
		encoded, hash, err := behavioralManifestJSON(c.Manifest)
		if c.ExpectedValid {
			if err != nil {
				t.Errorf("case %q: expected valid manifest, got error: %v", c.Name, err)
				continue
			}
			expected, err := json.Marshal(c.Manifest)
			if err != nil {
				t.Fatalf("case %q: marshal expected manifest: %v", c.Name, err)
			}
			if string(encoded) != string(expected) {
				t.Errorf("case %q: encoded bytes differ from json.Marshal(row.Manifest)", c.Name)
			}
			sum := sha256.Sum256(encoded)
			if hash != hex.EncodeToString(sum[:]) {
				t.Errorf("case %q: hash mismatch: got %q want %q", c.Name, hash, hex.EncodeToString(sum[:]))
			}
		} else {
			if err == nil {
				t.Errorf("case %q: expected invalid manifest, got success", c.Name)
				continue
			}
			if !errors.Is(err, ErrBehavioralRunBindings) {
				t.Errorf("case %q: expected ErrBehavioralRunBindings, got %v", c.Name, err)
			}
			if len(encoded) != 0 {
				t.Errorf("case %q: expected zero encoded bytes, got %d", c.Name, len(encoded))
			}
			if hash != "" {
				t.Errorf("case %q: expected empty hash, got %q", c.Name, hash)
			}
		}
	}

	bindingData, err := os.ReadFile("behavioral_run_binding_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_run_binding_cases.json: %v", err)
	}
	var bindingCases behavioralRunBindingCases
	if err := json.Unmarshal(bindingData, &bindingCases); err != nil {
		t.Fatalf("parse behavioral_run_binding_cases.json: %v", err)
	}
	if err := validateBehavioralRunBindings(bindingCases.OversizeBindings); err != nil {
		t.Fatalf("oversize_bindings fixture should validate: %v", err)
	}

	validFixture := cases[0].Manifest
	validFixture.Bindings = bindingCases.OversizeBindings
	encoded, hash, err := behavioralManifestJSON(validFixture)
	if err == nil {
		t.Errorf("oversize manifest: expected ErrBehavioralRunBindings, got success")
	} else if !errors.Is(err, ErrBehavioralRunBindings) {
		t.Errorf("oversize manifest: expected ErrBehavioralRunBindings, got %v", err)
	} else if len(encoded) != 0 {
		t.Errorf("oversize manifest: expected zero encoded bytes, got %d", len(encoded))
	} else if hash != "" {
		t.Errorf("oversize manifest: expected empty hash, got %q", hash)
	}
}
