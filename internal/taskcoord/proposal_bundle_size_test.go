package taskcoord

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"hermetrix-harness/internal/worker"
)

func TestProposalBundleSizeEnvelope(t *testing.T) {
	limit := 16 * 1024 * 1024

	for _, size := range []int{15 * 1024 * 1024, 16 * 1024 * 1024} {
		var summary strings.Builder
		for i := 0; i < size; i++ {
			summary.WriteByte('x')
		}

		bundle := proposalBundle{
			Version:         2,
			InputArtifactID: "artifact_fixture",
			Result: worker.Result{
				Status:      "proposed_unverified",
				ProposalID:  "proposal-1",
				InputSHA256: "sha256",
				Summary:     summary.String(),
				Changes: []worker.Change{{
					Path:         "internal/taskcoord/proposal_bundle_size_test.go",
					BeforeSHA256: "sha256",
					Content:      "x",
					Strategy:     "replace",
					Edits:        []worker.TextEdit{{Old: "x", New: "y"}},
				}},
			},
		}

		body, err := json.Marshal(bundle)
		if err != nil {
			t.Fatalf("marshal %dMiB bundle: %v", size/(1024*1024), err)
		}

		if size == 15*1024*1024 && len(body) >= limit {
			t.Fatalf("15MiB encoded body %d not below fixed 16MiB limit", len(body))
		}
		if size == 16*1024*1024 && len(body) <= limit {
			t.Fatalf("16MiB encoded body %d not above fixed 16MiB limit due JSON overhead", len(body))
		}

		decoded, err := decodeProposalBundle(body)
		if size == 15*1024*1024 {
			if err != nil {
				t.Fatalf("decode 15MiB bundle: %v", err)
			}
			if decoded.Version != 2 {
				t.Fatalf("15MiB decoded version = %d, want 2", decoded.Version)
			}
		}
		if size == 16*1024*1024 {
			if err == nil {
				t.Fatalf("decode 16MiB bundle succeeded, want error")
			}
			if !reflect.DeepEqual(decoded, proposalBundle{}) {
				t.Fatalf("16MiB decoded bundle not zero: %+v", decoded)
			}
		}
	}
}
