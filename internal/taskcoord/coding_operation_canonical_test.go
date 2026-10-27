package taskcoord

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"hermetrix-harness/internal/taskengine"
)

type defaultCase struct {
	Field    string `json:"field"`
	Default  int    `json:"default"`
	Explicit int    `json:"explicit"`
}

func TestCodingOperationFrozenDefaults(t *testing.T) {
	data, err := os.ReadFile("coding_operation_default_cases.json")
	if err != nil {
		t.Fatalf("read default cases: %v", err)
	}
	var cases []defaultCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("unmarshal default cases: %v", err)
	}
	if len(cases) != 5 {
		t.Fatalf("expected 5 default cases, got %d", len(cases))
	}
	seen := make(map[string]bool)
	for _, c := range cases {
		if seen[c.Field] {
			t.Fatalf("duplicate field %q", c.Field)
		}
		seen[c.Field] = true
		field, ok := reflect.TypeOf(StartGoalInput{}).FieldByName(c.Field)
		if !ok || field.Type.Kind() != reflect.Int {
			t.Fatalf("field %q is not a reflect.Int field on Goal", c.Field)
		}
	}
	for _, c := range cases {
		input := CodingOperationInput{
			OperationID: "valid",
			Task:        taskengine.CreateTaskInput{ProjectID: "project", Actor: "owner"},
			Goal:        StartGoalInput{},
		}
		out, raw, sha, err := canonicalCodingInput(input)
		if err != nil {
			t.Fatalf("zeroed %q: %v", c.Field, err)
		}
		if len(raw) == 0 {
			t.Fatalf("zeroed %q: raw is empty", c.Field)
		}
		if len(sha) != 64 {
			t.Fatalf("zeroed %q: sha is empty", c.Field)
		}
		got := reflect.ValueOf(out.Goal).FieldByName(c.Field).Int()
		if got != int64(c.Default) {
			t.Fatalf("zeroed %q: expected default %d, got %d", c.Field, c.Default, got)
		}
		input.Goal = StartGoalInput{}
		reflect.ValueOf(&input.Goal).Elem().FieldByName(c.Field).SetInt(int64(c.Explicit))
		out2, raw2, sha2, err := canonicalCodingInput(input)
		if err != nil {
			t.Fatalf("explicit %q: %v", c.Field, err)
		}
		if len(raw2) == 0 {
			t.Fatalf("explicit %q: raw is empty", c.Field)
		}
		if len(sha2) != 64 {
			t.Fatalf("explicit %q: sha is empty", c.Field)
		}
		if sha2 == sha {
			t.Fatalf("explicit %q: sha unchanged", c.Field)
		}
		got2 := reflect.ValueOf(out2.Goal).FieldByName(c.Field).Int()
		if got2 != int64(c.Explicit) {
			t.Fatalf("explicit %q: expected %d, got %d", c.Field, c.Explicit, got2)
		}
	}
}
