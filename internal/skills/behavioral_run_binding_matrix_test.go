package skills

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

type behavioralRunBindingCase struct {
	Name          string                `json:"name"`
	ExpectedValid bool                  `json:"expected_valid"`
	Bindings      BehavioralRunBindings `json:"bindings"`
}

type behavioralRunBindingCasesFile struct {
	BindingsCases []behavioralRunBindingCase `json:"bindings_cases"`
}

func TestBehavioralRunBindingsMatrix(t *testing.T) {
	data, err := os.ReadFile("behavioral_run_binding_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_run_binding_cases.json: %v", err)
	}
	var file behavioralRunBindingCasesFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal behavioral_run_binding_cases.json: %v", err)
	}
	if len(file.BindingsCases) != 34 {
		t.Fatalf("expected 34 bindings cases, got %d", len(file.BindingsCases))
	}
	var positive, negative int
	for _, row := range file.BindingsCases {
		if row.ExpectedValid {
			positive++
		} else {
			negative++
		}
		err := validateBehavioralRunBindings(row.Bindings)
		if row.ExpectedValid {
			if err != nil {
				t.Errorf("case %q expected valid bindings, got %v", row.Name, err)
			}
		} else {
			if !errors.Is(err, ErrBehavioralRunBindings) {
				t.Errorf("case %q expected ErrBehavioralRunBindings, got %v", row.Name, err)
			}
		}
	}
	if positive != 5 {
		t.Errorf("expected 5 positive cases, got %d", positive)
	}
	if negative != 29 {
		t.Errorf("expected 29 negative cases, got %d", negative)
	}
}
