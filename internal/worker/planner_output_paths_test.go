package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/store"
)

func plannerService(t *testing.T, comp providers.Completion, onRequest func(providers.ChatRequest)) (*providers.Service, providers.Profile) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := providers.NewService(db, reviewAdapter{completion: comp, onRequest: onRequest})
	prof, err := svc.Save(ctx, providers.SaveInput{Name: "planner", BaseURL: "https://plan.example/v1", Model: "test", ContextWindow: 98304, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	return svc, prof
}

func planJSON(outputPaths []string) string {
	step := map[string]any{
		"key": "s1", "title": "Step", "workspace_change": "Change a", "instructions": "Do work",
		"requirement_ids": []string{"AC-1"}, "dependencies": []string{},
		"checks":       []string{"go test ./..."},
		"effect_scope": []string{"provider.select_files", "provider.propose", "workspace.apply", "workspace.run", "provider.review"},
	}
	if outputPaths != nil {
		step["output_paths"] = outputPaths
	}
	b, _ := json.Marshal(map[string]any{"reason": "plan", "steps": []any{step}})
	return string(b)
}

func TestPlannerOutputScopeLegacyNilWritableBlankOutputs(t *testing.T) {
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON(nil)}}}
	svc, prof := plannerService(t, comp, nil)
	res, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("legacy: steps len=%d: %+v", len(res.Steps), res.Steps)
	}
	if len(res.Steps[0].OutputPaths) != 0 {
		t.Fatalf("legacy: outputs: %+v", res.Steps[0].OutputPaths)
	}
}

func TestPlannerOutputScopeBoundedWritableSubset(t *testing.T) {
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON([]string{"a"})}}}
	svc, prof := plannerService(t, comp, nil)
	res, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: []string{"a", "b"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 {
		t.Fatalf("subset: steps len=%d: %+v", len(res.Steps), res.Steps)
	}
	if len(res.Steps[0].OutputPaths) != 1 || res.Steps[0].OutputPaths[0] != "a" {
		t.Fatalf("outputs: %v", res.Steps[0].OutputPaths)
	}
}

func TestPlannerOutputScopeHostRejected(t *testing.T) {
	cases := []struct {
		name string
		wr   []string
		op   []string
	}{
		{"unauthorized", []string{"a"}, []string{"x"}},
		{"duplicate", []string{"a"}, []string{"a", "a"}},
		{"malformed", []string{"a"}, []string{"../a"}},
		{"explicitNilWritable", nil, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON(tc.op)}}}
			svc, prof := plannerService(t, comp, nil)
			_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: tc.wr}, Options{})
			if err == nil {
				t.Fatal("expected host rejection")
			}
		})
	}
}

func TestPlannerOutputScopeGlobalWritableLimit(t *testing.T) {
	var dispatched int
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON([]string{"a"})}}}
	svc, prof := plannerService(t, comp, func(providers.ChatRequest) { dispatched++ })
	writable := make([]string, 513)
	for i := range writable {
		writable[i] = fmt.Sprintf("f%03d", i)
	}
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: writable}, Options{})
	if err == nil {
		t.Fatal("513 writable paths should be rejected")
	}
	if dispatched != 0 {
		t.Fatal("dispatched before validation")
	}
}

func TestPlannerOutputScopeGlobalMalformedPath(t *testing.T) {
	var dispatched int
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON([]string{"a"})}}}
	svc, prof := plannerService(t, comp, func(providers.ChatRequest) { dispatched++ })
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: []string{"../evil"}}, Options{})
	if err == nil {
		t.Fatal("malformed writable path should be rejected")
	}
	if dispatched != 0 {
		t.Fatal("dispatched before validation")
	}
}

func TestPlannerOutputScopeGlobal33MissingOutputs(t *testing.T) {
	writable := make([]string, 33)
	for i := range writable {
		writable[i] = fmt.Sprintf("f%02d", i)
	}
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON(nil)}}}
	svc, prof := plannerService(t, comp, nil)
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: writable}, Options{})
	if err == nil {
		t.Fatal("non-empty writable without outputs should reject")
	}
}

func TestPlannerOutputScopeGlobal33BoundedOutput(t *testing.T) {
	writable := make([]string, 33)
	for i := range writable {
		writable[i] = fmt.Sprintf("f%02d", i)
	}
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON([]string{"f00"})}}}
	svc, prof := plannerService(t, comp, nil)
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: writable}, Options{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlannerOutputScope33StepOutputsReject(t *testing.T) {
	writable := make([]string, 40)
	for i := range writable {
		writable[i] = fmt.Sprintf("f%02d", i)
	}
	ops := make([]string, 33)
	for i := range ops {
		ops[i] = fmt.Sprintf("f%02d", i)
	}
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON(ops)}}}
	svc, prof := plannerService(t, comp, nil)
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}, WritablePaths: writable}, Options{})
	if err == nil {
		t.Fatal("33 output paths should exceed max 32")
	}
}

func TestPlannerOutputScopeSchemaCapture(t *testing.T) {
	var req providers.ChatRequest
	comp := providers.Completion{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{{Name: "submit_plan", Arguments: planJSON(nil)}}}
	svc, prof := plannerService(t, comp, func(r providers.ChatRequest) { req = r })
	_, err := PlanWithProviderService(context.Background(), svc, prof, PlanTask{TaskID: "t", Objective: "o", OriginalRequest: "r", Criteria: []string{"AC-1"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) == 0 {
		t.Fatal("tools missing: no tools in captured request")
	}
	var params map[string]any
	b, err := json.Marshal(req.Tools[0].Function.Parameters)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if err := json.Unmarshal(b, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties missing or malformed")
	}
	steps, ok := props["steps"].(map[string]any)
	if !ok {
		t.Fatal("steps missing or malformed")
	}
	items, ok := steps["items"].(map[string]any)
	if !ok {
		t.Fatal("items missing or malformed")
	}
	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("items properties missing or malformed")
	}
	op, ok := itemProps["output_paths"].(map[string]any)
	if !ok {
		t.Fatal("output_paths missing or malformed")
	}
	if op["type"] != "array" || op["minItems"] != float64(1) || op["maxItems"] != float64(32) || op["uniqueItems"] != true {
		t.Fatalf("output_paths schema: %+v", op)
	}
	opItems, ok := op["items"].(map[string]any)
	if !ok {
		t.Fatal("output_paths items missing or malformed")
	}
	if opItems["type"] != "string" {
		t.Fatal("items not string")
	}
	reqList, ok := items["required"].([]any)
	if !ok {
		t.Fatal("required list missing or malformed")
	}
	for _, r := range reqList {
		if r == "output_paths" {
			t.Fatal("output_paths must not be in required")
		}
	}
}
