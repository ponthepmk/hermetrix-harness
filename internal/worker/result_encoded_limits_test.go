package worker

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodedResultLimits(t *testing.T) {
	for _, n := range []int{2, 3} {
		result := resultResourceFixture()
		result.Changes = make([]Change, n)
		for i := 0; i < n; i++ {
			result.Changes[i] = Change{
				Path:         "small/path" + string(rune('a'+i)),
				BeforeSHA256: "abc123",
				Content:      strings.Repeat("<", 1024*1024),
				Strategy:     "replace_text",
			}
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if n == 2 {
			if len(data) >= 16*1024*1024 {
				t.Fatalf("n=2 encoded length %d not below limit", len(data))
			}
			if err := validateWorkerResultResources(result); err != nil {
				t.Fatalf("n=2 validator: %v", err)
			}
		} else {
			if len(data) <= 16*1024*1024 {
				t.Fatalf("n=3 encoded length %d not above limit", len(data))
			}
			if err := validateWorkerResultResources(result); err == nil {
				t.Fatalf("n=3 validator should fail")
			}
		}
	}
}
