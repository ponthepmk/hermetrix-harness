package taskcoord

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func TestHistoricalSourceSharedBudgetFrozenCases(t *testing.T) {
	cases := loadHistoricalBudgetCases(t)
	totalCalls := 0
	for _, c := range cases {
		budget := newHistoricalSourceBudget()
		for _, call := range c.Calls {
			totalCalls++
			changes := make([]worker.Change, len(call.Changes))
			for i, d := range call.Changes {
				content := strings.Repeat(d.Unit, d.Repeat)
				changes[i] = worker.Change{Path: d.Path, Content: content}
			}
			beforeChanges, err := json.Marshal(changes)
			if err != nil {
				t.Fatalf("marshal changes: %v", err)
			}
			beforePaths, err := json.Marshal(call.WritablePaths)
			if err != nil {
				t.Fatalf("marshal paths: %v", err)
			}
			records, omitted, truncated := budget.excerpts(changes, call.WritablePaths)
			if len(records) != call.Want.FileCount {
				t.Fatalf("file count: got %d want %d", len(records), call.Want.FileCount)
			}
			var byteCount int
			for _, r := range records {
				byteCount += int(r["excerpt_bytes"].(int))
			}
			if byteCount != call.Want.ByteCount {
				t.Fatalf("byte count: got %d want %d", byteCount, call.Want.ByteCount)
			}
			if omitted != call.Want.Omitted {
				t.Fatalf("omitted: got %d want %d", omitted, call.Want.Omitted)
			}
			if truncated != call.Want.Truncated {
				t.Fatalf("truncated: got %v want %v", truncated, call.Want.Truncated)
			}
			for _, r := range records {
				path := r["path"].(string)
				var writable bool
				for _, wp := range call.WritablePaths {
					if wp == path {
						writable = true
						break
					}
				}
				if !writable {
					t.Fatalf("record path %q not writable", path)
				}
				fullSHA := r["full_sha256"].(string)
				fullBytes := int(r["full_bytes"].(int))
				excerptBytes := int(r["excerpt_bytes"].(int))
				excerpt := r["excerpt"].(string)
				trunc := r["truncated"].(bool)
				if len(excerpt) != excerptBytes {
					t.Fatalf("excerpt_bytes mismatch: got %d want %d", len(excerpt), excerptBytes)
				}
				if !utf8.ValidString(excerpt) {
					t.Fatalf("excerpt not valid UTF-8")
				}
				if trunc != (excerptBytes < fullBytes) {
					t.Fatalf("truncated mismatch: got %v want %v", trunc, excerptBytes < fullBytes)
				}
				var descriptor *descriptor
				for _, d := range call.Changes {
					if d.Path == path {
						descriptor = &d
						break
					}
				}
				if descriptor == nil {
					t.Fatalf("no descriptor for path %q", path)
				}
				if fullSHA != descriptor.FullSHA256 {
					t.Fatalf("sha mismatch for %q", path)
				}
				if fullBytes != descriptor.FullBytes {
					t.Fatalf("full_bytes mismatch for %q: got %d want %d", path, fullBytes, descriptor.FullBytes)
				}
				content := strings.Repeat(descriptor.Unit, descriptor.Repeat)
				if len(excerpt) > 0 && !bytes.HasPrefix([]byte(content), []byte(excerpt)) {
					t.Fatalf("excerpt not prefix of content for %q", path)
				}
			}
			afterChanges, err := json.Marshal(changes)
			if err != nil {
				t.Fatalf("marshal after changes: %v", err)
			}
			afterPaths, err := json.Marshal(call.WritablePaths)
			if err != nil {
				t.Fatalf("marshal after paths: %v", err)
			}
			if !bytes.Equal(beforeChanges, afterChanges) {
				t.Fatalf("changes mutated")
			}
			if !bytes.Equal(beforePaths, afterPaths) {
				t.Fatalf("paths mutated")
			}
			if budget.remainingFiles < 0 || budget.remainingBytes < 0 {
				t.Fatalf("budget went negative: remainingFiles=%d remainingBytes=%d", budget.remainingFiles, budget.remainingBytes)
			}
		}
	}
	if totalCalls != 14 {
		t.Fatalf("total calls: got %d want 14", totalCalls)
	}
}
