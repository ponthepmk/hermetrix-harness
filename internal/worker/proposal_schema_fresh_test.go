package worker

import "testing"

func TestProposalSchemaFreshCopies(t *testing.T) {
	task := Task{
		Brief:         "nonempty brief",
		Files:         map[string]string{"a.go": "x"},
		WritablePaths: []string{"a.go"},
	}
	req1 := proposalRequest(task, nil)
	req2 := proposalRequest(task, nil)

	req1Changes := req1.Tools[0].Function.Parameters["properties"].(map[string]any)["changes"].(map[string]any)
	req2Changes := req2.Tools[0].Function.Parameters["properties"].(map[string]any)["changes"].(map[string]any)

	req2Changes["maxItems"] = 999

	if req1Changes["maxItems"] != 1 {
		t.Fatalf("req1 changes.maxItems = %v, want 1", req1Changes["maxItems"])
	}

	req3 := proposalRequest(task, nil)
	req3Changes := req3.Tools[0].Function.Parameters["properties"].(map[string]any)["changes"].(map[string]any)
	if req3Changes["maxItems"] != 1 {
		t.Fatalf("req3 changes.maxItems = %v, want 1", req3Changes["maxItems"])
	}
}
