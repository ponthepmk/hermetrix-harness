package worker

import (
	"context"
	"hermetrix-harness/internal/providers"
	"testing"
)

func TestNewFileProposalBindsAbsenceSeparatelyFromEmptyExistingFile(t *testing.T) {
	task := Task{ID: "create", Brief: "create implementation", Files: map[string]string{"new.js": ""}, WritablePaths: []string{"new.js"}, AbsentFiles: []string{"new.js"}}
	f := &fake{arguments: `{"summary":"create","changes":[{"path":"new.js","content":"module.exports=42;"}]}`}
	result, err := Run(context.Background(), f, providers.Profile{}, "", task)
	if err != nil || len(result.Changes) != 1 || result.Changes[0].BeforeSHA256 != "absent" || result.Changes[0].Strategy != "create_file" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err = ValidateResult(task, result); err != nil {
		t.Fatal(err)
	}
	existing := task
	existing.AbsentFiles = nil
	if err = ValidateResult(existing, result); err == nil {
		t.Fatal("creation proposal passed against an existing empty file")
	}
	for _, absent := range [][]string{{"unseen.js"}, {"new.js", "new.js"}} {
		bad := task
		bad.AbsentFiles = absent
		if _, err = Run(context.Background(), f, providers.Profile{}, "", bad); err == nil {
			t.Fatal("forged absent snapshot accepted")
		}
	}
	bad := task
	bad.WritablePaths = nil
	if _, err = Run(context.Background(), f, providers.Profile{}, "", bad); err == nil {
		t.Fatal("new file lacks explicit write scope")
	}
}
