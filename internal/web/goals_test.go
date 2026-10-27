package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/taskengine"
)

func TestGoalAPIStartsReadsStopsAndRejectsStaleResume(t *testing.T) {
	s := testConfiguredServer(t)
	ctx := context.Background()
	project, err := s.product.EnsureWorkspaceProject(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.tasks.Create(ctx, taskengine.CreateTaskInput{ProjectID: project.ID, Title: "Goal API", Objective: "verify exact output", OriginalRequest: "build and verify", Actor: "owner", Criteria: []taskengine.Criterion{{ID: "AC-1", Description: "actual check passes"}}, Contract: taskengine.GoalContract{WritablePaths: []string{"result.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.providers.Save(ctx, providers.SaveInput{Name: "writer", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 32768})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.providers.Save(ctx, providers.SaveInput{Name: "reviewer", BaseURL: "http://127.0.0.1:8088/v1", Model: "fixture", ContextWindow: 32768})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	requestJSON(t, server.URL+"/api/goals/missing", http.MethodGet, nil, http.StatusNotFound)
	body := requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/goals", http.MethodPost, map[string]any{"expected_task_revision": task.Revision, "actor": "owner", "provider_id": w.ID, "reviewer_provider_id": r.ID, "apply_changes": true}, http.StatusCreated)
	var g taskengine.GoalRun
	if err = json.Unmarshal(body, &g); err != nil || g.State != taskengine.GoalActive {
		t.Fatalf("goal=%s err=%v", body, err)
	}
	requestJSON(t, server.URL+"/api/goals/"+g.ID, http.MethodGet, nil, http.StatusOK)
	listed := requestJSON(t, server.URL+"/api/tasks/"+task.ID+"/goals", http.MethodGet, nil, http.StatusOK)
	var items []taskengine.GoalRun
	if err = json.Unmarshal(listed, &items); err != nil || len(items) != 1 || items[0].ID != g.ID {
		t.Fatalf("lost-response recovery list=%s err=%v", listed, err)
	}
	if _, err = s.tasks.WaitGoal(ctx, g.ID, "owner review required"); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.URL+"/api/goals/"+g.ID+"/resume", http.MethodPost, map[string]any{"expected_task_revision": task.Revision + 99}, http.StatusConflict)
	requestJSON(t, server.URL+"/api/goals/"+g.ID+"/resume", http.MethodPost, map[string]any{"expected_task_revision": task.Revision}, http.StatusOK)
	body = requestJSON(t, server.URL+"/api/goals/"+g.ID+"/stop", http.MethodPost, map[string]any{}, http.StatusOK)
	if err = json.Unmarshal(body, &g); err != nil || g.State != taskengine.GoalStopped {
		t.Fatalf("stop=%s err=%v", body, err)
	}
}
