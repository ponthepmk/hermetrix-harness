package taskcoord

import (
	"unicode/utf8"

	"hermetrix-harness/internal/worker"
)

func historicalSourceExcerpts(changes []worker.Change, writablePaths []string) ([]map[string]any, int, bool) {
	records := []map[string]any{}
	omitted := 0
	truncated := false
	remaining := 8192
	maxRecords := 2

	for _, change := range changes {
		allowed := false
		for _, p := range writablePaths {
			if p == change.Path {
				allowed = true
				break
			}
		}
		if !allowed {
			continue
		}
		if len(records) >= maxRecords || remaining <= 0 {
			omitted++
			truncated = true
			continue
		}
		fullBytes := len(change.Content)
		fullSHA := worker.Hash(change.Content)
		if fullBytes == 0 {
			records = append(records, map[string]any{
				"path":          change.Path,
				"full_sha256":   fullSHA,
				"full_bytes":    fullBytes,
				"excerpt_bytes": 0,
				"excerpt":       "",
				"truncated":     false,
			})
			continue
		}
		excerpt := change.Content
		if fullBytes > remaining {
			for len(excerpt) > remaining && len(excerpt) > 0 {
				excerpt = excerpt[:len(excerpt)-1]
			}
			for len(excerpt) > 0 && !utf8.ValidString(excerpt) {
				excerpt = excerpt[:len(excerpt)-1]
			}
			if len(excerpt) == 0 {
				omitted++
				truncated = true
				continue
			}
			truncated = true
		}
		records = append(records, map[string]any{
			"path":          change.Path,
			"full_sha256":   fullSHA,
			"full_bytes":    fullBytes,
			"excerpt_bytes": len(excerpt),
			"excerpt":       excerpt,
			"truncated":     len(excerpt) < fullBytes,
		})
		remaining -= len(excerpt)
	}
	return records, omitted, truncated
}
