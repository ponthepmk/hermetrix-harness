package skills

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

type decodeCase struct {
	Name          string `json:"name"`
	Body          string `json:"body"`
	Hash          string `json:"hash"`
	ExpectedValid bool   `json:"expected_valid"`
}

type stateCase struct {
	Name          string        `json:"name"`
	Run           BehavioralRun `json:"run"`
	ExpectedValid bool          `json:"expected_valid"`
}

type matrixFile struct {
	Decode []decodeCase `json:"decode"`
	States []stateCase  `json:"states"`
}

func TestBehavioralRunRecordCases(t *testing.T) {
	data, err := os.ReadFile("behavioral_run_record_cases.json")
	if err != nil {
		t.Fatalf("read behavioral_run_record_cases.json: %v", err)
	}
	var file matrixFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal behavioral_run_record_cases.json: %v", err)
	}
	if len(file.Decode) != 14 {
		t.Fatalf("expected 14 decode cases, got %d", len(file.Decode))
	}
	if len(file.States) != 31 {
		t.Fatalf("expected 31 states cases, got %d", len(file.States))
	}
	var decodeValid, stateValid int
	for _, row := range file.Decode {
		if row.ExpectedValid {
			decodeValid++
		}
		m, err := decodeBehavioralRunManifest(row.Body, row.Hash)
		if row.ExpectedValid {
			if err != nil {
				t.Errorf("decode case %q expected valid, got %v", row.Name, err)
			}
			var expected BehavioralRunManifest
			if err := json.Unmarshal([]byte(row.Body), &expected); err != nil {
				t.Fatalf("decode case %q expected body unmarshal: %v", row.Name, err)
			}
			if !reflect.DeepEqual(m, expected) {
				t.Errorf("decode case %q manifest mismatch", row.Name)
			}
		} else {
			if !errors.Is(err, ErrBehavioralRunBindings) {
				t.Errorf("decode case %q expected ErrBehavioralRunBindings, got %v", row.Name, err)
			}
			if !reflect.DeepEqual(m, BehavioralRunManifest{}) {
				t.Errorf("decode case %q returned non-zero manifest", row.Name)
			}
		}
	}
	if decodeValid != 2 {
		t.Errorf("expected 2 valid decode cases, got %d", decodeValid)
	}
	for _, row := range file.States {
		if row.ExpectedValid {
			stateValid++
		}
		err := validateBehavioralRunState(row.Run)
		if row.ExpectedValid {
			if err != nil {
				t.Errorf("state case %q expected valid, got %v", row.Name, err)
			}
		} else {
			if !errors.Is(err, ErrBehavioralRunBindings) {
				t.Errorf("state case %q expected ErrBehavioralRunBindings, got %v", row.Name, err)
			}
		}
	}
	if stateValid != 9 {
		t.Errorf("expected 9 valid states cases, got %d", stateValid)
	}
}
