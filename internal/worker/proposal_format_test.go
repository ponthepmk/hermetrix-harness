package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"hermetrix-harness/internal/providers"
)

func compileSchema(t *testing.T, schema map[string]any) *jsonschema.Schema {
	t.Helper()
	raw, _ := json.Marshal(schema)
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	loc := "urn:test:schema"
	if err := c.AddResource(loc, doc); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	s, err := c.Compile(loc)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

func validateInstance(t *testing.T, s *jsonschema.Schema, raw string) error {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode instance: %v", err)
	}
	return s.Validate(v)
}

func captureSchema(t *testing.T, task Task) map[string]any {
	t.Helper()
	args := `{"changes":[{"path":"x","content":"new"}]}`
	if task.ProposalFormat == "edits_only" {
		args = `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`
	}
	f := &fake{arguments: args}
	if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return f.request.Tools[0].Function.Parameters
}

func TestProposalFormatBlankLegacyBehavior(t *testing.T) {
	f := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "", Task{Brief: "fix", Files: map[string]string{"x": "old"}})
	if err != nil || r.Changes[0].Strategy != "replace_file" {
		t.Fatalf("content strategy: %v %v", r, err)
	}
	f = &fake{arguments: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`}
	r, err = Run(context.Background(), f, providers.Profile{}, "", Task{Brief: "fix", Files: map[string]string{"x": "old"}})
	if err != nil || r.Changes[0].Strategy != "replace_text" {
		t.Fatalf("edits strategy: %v %v", r, err)
	}
	f = &fake{arguments: `{"changes":[{"path":"n.go","content":"c"}]}`}
	r, err = Run(context.Background(), f, providers.Profile{}, "", Task{Brief: "fix", Files: map[string]string{"x": "old", "n.go": ""}, WritablePaths: []string{"n.go"}, AbsentFiles: []string{"n.go"}})
	if err != nil || r.Changes[0].Strategy != "create_file" {
		t.Fatalf("absent file: %v %v", r, err)
	}
}

func TestProposalFormatDefaultSchemaMatchesLegacy(t *testing.T) {
	base := Task{Brief: "fix", Files: map[string]string{"x": "old"}}
	if !reflect.DeepEqual(captureSchema(t, base), proposalSchema()) {
		t.Fatal("default schema != proposalSchema()")
	}
	_ = captureSchema(t, Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"})
	if !reflect.DeepEqual(captureSchema(t, base), proposalSchema()) {
		t.Fatal("default schema changed after specialized call")
	}
}

func TestProposalFormatDefaultTaskJSONAndHash(t *testing.T) {
	task := Task{Brief: "fix", Files: map[string]string{"x": "old"}}
	f := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(task)
	const want = `{"brief":"fix","files":{"x":"old"}}`
	if string(body) != want {
		t.Fatalf("task JSON = %s, want %s", body, want)
	}
	if f.request.Messages[1].Content != want {
		t.Fatalf("request body = %s", f.request.Messages[1].Content)
	}
	if r.InputSHA256 != Hash(want) {
		t.Fatalf("InputSHA256 = %s, want %s", r.InputSHA256, Hash(want))
	}
}

func TestProposalFormatNonBlankChangesTaskHash(t *testing.T) {
	base := Task{Brief: "fix", Files: map[string]string{"x": "old"}}
	edits := base
	edits.ProposalFormat = "edits_only"
	b1, _ := json.Marshal(base)
	b2, _ := json.Marshal(edits)
	if string(b1) == string(b2) || Hash(string(b1)) == Hash(string(b2)) {
		t.Fatal("nonblank mode did not change task hash")
	}
}

func TestProposalFormatSystemMessageGuidance(t *testing.T) {
	const fallback = "Prefer exact text edits when they are unambiguous; use complete replacement content otherwise."
	base := Task{Brief: "fix", Files: map[string]string{"x": "old"}}
	f := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	if _, err := Run(context.Background(), f, providers.Profile{}, "", base); err != nil {
		t.Fatalf("blank Run: %v", err)
	}
	if !strings.Contains(f.request.Messages[0].Content, fallback) {
		t.Fatalf("blank system message lost fallback sentence: %s", f.request.Messages[0].Content)
	}
	cases := []struct {
		mode string
		args string
	}{
		{mode: "edits_only", args: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`},
		{mode: "content_only", args: `{"changes":[{"path":"x","content":"new"}]}`},
	}
	for _, c := range cases {
		task := base
		task.ProposalFormat = c.mode
		f := &fake{arguments: c.args}
		if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err != nil {
			t.Fatalf("%s Run: %v", c.mode, err)
		}
		sys := f.request.Messages[0].Content
		if strings.Contains(sys, fallback) {
			t.Fatalf("%s system message retains fallback sentence: %s", c.mode, sys)
		}
		if !strings.Contains(sys, c.mode) {
			t.Fatalf("%s system message missing mode identifier: %s", c.mode, sys)
		}
	}
}

func TestProposalFormatUnknownModeRejected(t *testing.T) {
	for _, mode := range []string{"private_mode", "unknown", "EDITS_ONLY"} {
		f := &fake{}
		_, err := Run(context.Background(), f, providers.Profile{}, "sk-secret", Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: mode})
		var d *Error
		if !errors.As(err, &d) || d.Code != ErrorInvalidTask || d.Phase != PhaseValidatingInput {
			t.Fatalf("mode %q: want ErrorInvalidTask/PhaseValidatingInput, got %v", mode, err)
		}
		if f.calls != 0 {
			t.Fatalf("mode %q: provider was called", mode)
		}
		msg := err.Error()
		if strings.Contains(msg, mode) || strings.Contains(msg, "sk-secret") {
			t.Fatalf("mode %q: leaked in error: %s", mode, msg)
		}
		raw, jerr := json.Marshal(d)
		if jerr != nil {
			t.Fatalf("mode %q: marshal structured error: %v", mode, jerr)
		}
		if strings.Contains(string(raw), mode) || strings.Contains(string(raw), "sk-secret") {
			t.Fatalf("mode %q: leaked in structured error JSON: %s", mode, raw)
		}
	}
}

func TestProposalFormatValidateResultUnknownMode(t *testing.T) {
	for _, mode := range []string{"private_mode", "unknown"} {
		task := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: mode}
		err := ValidateResult(task, Result{})
		var d *Error
		if !errors.As(err, &d) || d.Code != ErrorInvalidTask || d.Phase != PhaseValidatingInput {
			t.Fatalf("mode %q: want ErrorInvalidTask/PhaseValidatingInput before identity handling, got %v", mode, err)
		}
		if strings.Contains(err.Error(), mode) {
			t.Fatalf("mode %q: leaked in safe message: %s", mode, err.Error())
		}
	}
}

func TestProposalFormatEditsOnlyRejectsAbsentFiles(t *testing.T) {
	f := &fake{}
	_, err := Run(context.Background(), f, providers.Profile{}, "", Task{
		Brief: "fix", Files: map[string]string{"x": "old", "n.go": ""},
		WritablePaths: []string{"n.go"}, AbsentFiles: []string{"n.go"}, ProposalFormat: "edits_only",
	})
	var d *Error
	if !errors.As(err, &d) || d.Code != ErrorInvalidTask || d.Phase != PhaseValidatingInput {
		t.Fatalf("edits_only+AbsentFiles: want ErrorInvalidTask, got %v", err)
	}
	if f.calls != 0 {
		t.Fatal("provider was called")
	}
}

func TestProposalFormatEditsOnlyAcceptsEditsRejectsContent(t *testing.T) {
	task := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"}
	f := &fake{arguments: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil || r.Changes[0].Strategy != "replace_text" {
		t.Fatalf("edits_only accepted edits: %v %v", r, err)
	}
	f = &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err == nil {
		t.Fatal("edits_only accepted full content")
	}
}

func TestProposalFormatContentOnlyAcceptsContentRejectsEdits(t *testing.T) {
	task := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "content_only"}
	f := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil || r.Changes[0].Strategy != "replace_file" {
		t.Fatalf("content_only accepted content: %v %v", r, err)
	}
	f = &fake{arguments: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`}
	if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err == nil {
		t.Fatal("content_only accepted edits")
	}
}

func TestProposalFormatContentOnlyAcceptsAbsentFile(t *testing.T) {
	task := Task{Brief: "fix", Files: map[string]string{"x": "old", "n.go": ""}, WritablePaths: []string{"n.go"}, AbsentFiles: []string{"n.go"}, ProposalFormat: "content_only"}
	f := &fake{arguments: `{"changes":[{"path":"n.go","content":"c"}]}`}
	r, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil || r.Changes[0].Strategy != "create_file" {
		t.Fatalf("content_only absent file: %v %v", r, err)
	}
}

func TestProposalFormatBothNeitherStillRejected(t *testing.T) {
	for _, mode := range []string{"edits_only", "content_only"} {
		task := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: mode}
		f := &fake{arguments: `{"changes":[{"path":"x","content":"n","edits":[{"old":"o","new":"n"}]}]}`}
		if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err == nil {
			t.Fatalf("%s: both content+edits accepted", mode)
		}
		f = &fake{arguments: `{"changes":[{"path":"x"}]}`}
		if _, err := Run(context.Background(), f, providers.Profile{}, "", task); err == nil {
			t.Fatalf("%s: neither content nor edits accepted", mode)
		}
	}
}

func TestProposalFormatCapturedEditsSchema(t *testing.T) {
	s := compileSchema(t, captureSchema(t, Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"}))
	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"valid edits", `{"changes":[{"path":"x","edits":[{"old":"a","new":"b"}]}]}`, true},
		{"content only", `{"changes":[{"path":"x","content":"f"}]}`, false},
		{"both content and edits", `{"changes":[{"path":"x","content":"f","edits":[{"old":"a","new":"b"}]}]}`, false},
		{"missing edits", `{"changes":[{"path":"x"}]}`, false},
		{"empty edits", `{"changes":[{"path":"x","edits":[]}]}`, false},
		{"unknown field", `{"changes":[{"path":"x","edits":[{"old":"a","new":"b"}],"extra":1}]}`, false},
	}
	for _, c := range cases {
		err := validateInstance(t, s, c.raw)
		if c.ok && err != nil {
			t.Errorf("%s: rejected: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestProposalFormatCapturedContentSchema(t *testing.T) {
	s := compileSchema(t, captureSchema(t, Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "content_only"}))
	cases := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"valid content", `{"changes":[{"path":"x","content":"f"}]}`, true},
		{"edits present", `{"changes":[{"path":"x","edits":[{"old":"a","new":"b"}]}]}`, false},
		{"missing content", `{"changes":[{"path":"x"}]}`, false},
	}
	for _, c := range cases {
		err := validateInstance(t, s, c.raw)
		if c.ok && err != nil {
			t.Errorf("%s: rejected: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestProposalFormatSchemaRemovesForbiddenProperty(t *testing.T) {
	es := captureSchema(t, Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"})
	items := es["properties"].(map[string]any)["changes"].(map[string]any)["items"].(map[string]any)
	props := items["properties"].(map[string]any)
	if _, ok := props["content"]; ok {
		t.Fatal("edits_only schema retains 'content' property")
	}
	cs := captureSchema(t, Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "content_only"})
	items = cs["properties"].(map[string]any)["changes"].(map[string]any)["items"].(map[string]any)
	props = items["properties"].(map[string]any)
	if _, ok := props["edits"]; ok {
		t.Fatal("content_only schema retains 'edits' property")
	}
}

func TestProposalFormatForgedStrategyRejected(t *testing.T) {
	// edits_only: valid result validates
	et := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"}
	f := &fake{arguments: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`}
	er, err := Run(context.Background(), f, providers.Profile{}, "", et)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(et, er); err != nil {
		t.Fatalf("valid edits result: %v", err)
	}
	// forge opposite strategy
	fg := er
	fg.Changes = append([]Change(nil), er.Changes...)
	fg.Changes[0].Strategy = "replace_file"
	fg.ProposalID = proposalID(er.InputSHA256, fg)
	if err := ValidateResult(et, fg); err == nil {
		t.Fatal("forged replace_file accepted in edits_only")
	}
	// content_only: valid result validates
	ct := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "content_only"}
	f2 := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	cr, err := Run(context.Background(), f2, providers.Profile{}, "", ct)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(ct, cr); err != nil {
		t.Fatalf("valid content result: %v", err)
	}
	// forge opposite strategy
	fg2 := cr
	fg2.Changes = append([]Change(nil), cr.Changes...)
	fg2.Changes[0].Strategy = "replace_text"
	fg2.ProposalID = proposalID(cr.InputSHA256, fg2)
	if err := ValidateResult(ct, fg2); err == nil {
		t.Fatal("forged replace_text accepted in content_only")
	}
}

func TestProposalFormatValidStrategiesAndAbsenceStillValidate(t *testing.T) {
	et := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "edits_only"}
	f := &fake{arguments: `{"changes":[{"path":"x","edits":[{"old":"old","new":"new"}]}]}`}
	er, err := Run(context.Background(), f, providers.Profile{}, "", et)
	if err != nil || ValidateResult(et, er) != nil {
		t.Fatalf("edits valid: %v %v", er, err)
	}
	ct := Task{Brief: "fix", Files: map[string]string{"x": "old"}, ProposalFormat: "content_only"}
	f2 := &fake{arguments: `{"changes":[{"path":"x","content":"new"}]}`}
	cr, err := Run(context.Background(), f2, providers.Profile{}, "", ct)
	if err != nil || ValidateResult(ct, cr) != nil {
		t.Fatalf("content valid: %v %v", cr, err)
	}
	at := Task{Brief: "fix", Files: map[string]string{"x": "old", "n.go": ""}, WritablePaths: []string{"n.go"}, AbsentFiles: []string{"n.go"}, ProposalFormat: "content_only"}
	f3 := &fake{arguments: `{"changes":[{"path":"n.go","content":"c"}]}`}
	ar, err := Run(context.Background(), f3, providers.Profile{}, "", at)
	if err != nil || ValidateResult(at, ar) != nil {
		t.Fatalf("absent valid: %v %v", ar, err)
	}
}
