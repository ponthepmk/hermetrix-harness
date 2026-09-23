package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"hermetrix-harness/internal/product"
)

func TestDebuggerRoutesRequireAuthentication(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	auth := newAuthenticator(token, "local-user", false)
	server := httptest.NewServer(auth.middleware(testHandler(t)))
	defer server.Close()
	for _, path := range []string{"/api/debug/capabilities", "/api/debug/sessions?project_id=missing", "/api/debug/sessions/missing", "/api/debug/sessions/missing/variables"} {
		assertStatus(t, http.MethodGet, server.URL+path, "", "", http.StatusUnauthorized)
	}
	for _, route := range []struct{ method, path, body string }{{http.MethodPost, "/api/debug/sessions", `{"runtime":"node"}`}, {http.MethodPost, "/api/debug/sessions/missing/control", `{"action":"continue"}`}, {http.MethodPut, "/api/debug/sessions/missing/breakpoints", `{"breakpoints":[]}`}} {
		assertStatus(t, route.method, server.URL+route.path, route.body, "", http.StatusUnauthorized)
	}
	assertStatus(t, http.MethodGet, server.URL+"/api/debug/capabilities", "", "Bearer "+token, http.StatusOK)
}

func TestDebuggerHTTPRealBreakpointAndVariables(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js not installed")
	}
	t.Setenv("HERMETRIX_REQUIRE_OS_SANDBOX", "")
	server := testHTTPServer(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.cjs"), []byte("function run() {\n const answer = 42;\n console.log(answer);\n}\nrun();\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var project product.Project
	body := requestJSON(t, server.URL+"/api/projects", http.MethodPost, product.ProjectInput{Name: "Debug API", RootPath: root}, http.StatusCreated)
	if err := json.Unmarshal(body, &project); err != nil {
		t.Fatal(err)
	}
	var session product.DebugSession
	body = requestJSON(t, server.URL+"/api/debug/sessions", http.MethodPost, product.DebugStartInput{ProjectID: project.ID, Runtime: "node", Program: "main.cjs", Breakpoints: []product.DebugBreakpoint{{Path: "main.cjs", Line: 3}}}, http.StatusCreated)
	if err := json.Unmarshal(body, &session); err != nil {
		t.Fatal(err)
	}
	readUntil := func(line int) product.DebugSession {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			body := requestJSON(t, server.URL+"/api/debug/sessions/"+session.ID, http.MethodGet, nil, http.StatusOK)
			var item product.DebugSession
			if err := json.Unmarshal(body, &item); err != nil {
				t.Fatal(err)
			}
			if item.State == "paused" && (line == 0 || (len(item.Stack) > 0 && item.Stack[0].Line == line)) {
				return item
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("HTTP debugger did not pause at expected location")
		return product.DebugSession{}
	}
	readUntil(0)
	requestJSON(t, server.URL+"/api/debug/sessions/"+session.ID+"/control", http.MethodPost, map[string]string{"action": "continue"}, http.StatusOK)
	session = readUntil(3)
	body = requestJSON(t, server.URL+"/api/debug/sessions/"+session.ID+"/variables?frame_id="+session.Stack[0].ID, http.MethodGet, nil, http.StatusOK)
	var variables []product.DebugVariable
	if err := json.Unmarshal(body, &variables); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range variables {
		found = found || item.Name == "answer" && item.Value == "42"
	}
	if !found {
		t.Fatalf("real local variable missing: %s", body)
	}
	requestJSON(t, server.URL+"/api/debug/sessions/"+session.ID+"/breakpoints", http.MethodPut, map[string]any{"breakpoints": []product.DebugBreakpoint{{Path: "../outside.cjs", Line: 1}}}, http.StatusBadRequest)
	requestJSON(t, server.URL+"/api/debug/sessions/"+session.ID+"/control", http.MethodPost, map[string]string{"action": "stop"}, http.StatusOK)
}
