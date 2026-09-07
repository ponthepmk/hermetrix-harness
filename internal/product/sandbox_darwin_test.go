//go:build darwin

package product

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMacOSCommandSandboxBlocksNetworkBeforeServerHit(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer target.Close()
	service, _, _ := testProductService(t)
	project, err := service.SaveProject(context.Background(), ProjectInput{Name: "sandbox", RootPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("import urllib.request; urllib.request.urlopen(%q)", target.URL)
	job, err := service.StartCommand(context.Background(), CommandInput{ProjectID: project.ID, Actor: "test-user",
		Executable: "python3", Arguments: []string{"-c", script}, TimeoutSeconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	completed := waitForJob(t, service, job.ID)
	if completed.State != "failed" || hits.Load() != 0 {
		t.Fatalf("sandboxed network command state=%s hits=%d result=%+v", completed.State, hits.Load(), completed.Result)
	}
	sandbox, _ := completed.Result["sandbox"].(map[string]any)
	if sandbox["kind"] != "macos-seatbelt" || sandbox["network_isolated"] != true ||
		!strings.Contains(completed.Error, "sandbox") && !strings.Contains(completed.Result["output"].(string), "Operation not permitted") {
		t.Fatalf("network isolation receipt/error is incomplete: job=%+v sandbox=%+v", completed, sandbox)
	}
}
