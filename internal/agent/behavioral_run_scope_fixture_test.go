package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	skills "hermetrix-harness/internal/skills"
)

func behavioralRunScopeCandidate(t *testing.T, svc *skills.Service, kind, ref string) skills.Candidate {
	t.Helper()
	input := behavioralRunScopeCandidateInput(kind, ref)
	candidate, err := svc.CreateCandidate(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if candidate.State != skills.CandidateNeedsReview {
		t.Fatalf("candidate needs review")
	}
	if !candidate.Checks.Passed {
		t.Fatalf("candidate checks did not pass")
	}
	return candidate
}

func behavioralRunScopeCandidateInput(kind, ref string) skills.CreateCandidateInput {
	name := "scope-run-" + kind
	return skills.CreateCandidateInput{
		CanonicalName: name,
		ScopeKind:     kind,
		ScopeRef:      ref,
		Origin:        "user_created",
		Owner:         "user",
		ChangeKind:    "create",
		CreatedBy:     "user",
		TriggerKind:   "manual",
		Reason:        "fixture",
		EvidenceRefs:  []string{"session:test"},
		Files: []skills.File{{
			Path:    "tests/procedure.json",
			Content: []byte(`{"id":"procedure","prompt":"what does this procedure say","required_phrases":["#"]}`),
		}},
		Markdown: "---\nname: " + name + "\ndescription: \"Behavioral run scope fixture\"\ntags: [test]\ntools: [filesystem.read]\n---\n\n# Procedure\n\n1. Read evidence.\n2. Report risks.\n",
	}
}

func behavioralRunScopeBindings(t *testing.T) skills.BehavioralRunBindings {
	t.Helper()
	data, err := os.ReadFile("../skills/behavioral_run_binding_cases.json")
	if err != nil {
		t.Fatalf("read bindings cases: %v", err)
	}
	var wrapper struct {
		BindingsCases []struct {
			Bindings skills.BehavioralRunBindings `json:"bindings"`
			CaseIDs  []string                     `json:"case_ids"`
		} `json:"bindings_cases"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("parse bindings cases: %v", err)
	}
	if len(wrapper.BindingsCases) != 34 {
		t.Fatalf("expected 34 bindings cases, got %d", len(wrapper.BindingsCases))
	}
	first := wrapper.BindingsCases[0].Bindings
	if len(first.CaseIDs) == 0 {
		t.Fatalf("first bindings case has no case ids")
	}
	return first
}
