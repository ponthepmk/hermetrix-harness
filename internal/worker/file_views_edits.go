package worker

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// MaterializeBoundedChange validates bounded views and edits, then applies
// disjoint edits to the supplied full source. It returns the complete source
// and the strategy "replace_text" on success.
func MaterializeBoundedChange(expectedPath, fullSource, expectedFullSHA string, views []BoundedFileView, content *string, edits []TextEdit) (string, string, error) {
	if (content == nil) == (len(edits) == 0) {
		return "", "", errors.New("bounded changes require one or more text edits and no replacement content")
	}
	if content != nil {
		return "", "", errors.New("complete replacement content is not allowed in bounded mode")
	}
	if len(edits) > 64 {
		return "", "", errors.New("a change may contain at most 64 text edits")
	}
	if len(views) == 0 {
		return "", "", errors.New("no edit permission without at least one bounded view")
	}
	if err := validateBoundedViewSet(expectedPath, fullSource, expectedFullSHA, views); err != nil {
		return "", "", err
	}
	for i, e := range edits {
		if e.Old == "" {
			return "", "", errors.New(fmt.Sprintf("text edit %d has an empty preimage; provide nonempty exact text from an authorized visible region", i+1))
		}
		if !utf8.ValidString(e.Old) || !utf8.ValidString(e.New) {
			return "", "", errors.New(fmt.Sprintf("text edit %d has invalid UTF-8", i+1))
		}
	}
	type resolved struct {
		start, end int
		viewIndex  int
	}
	var res []resolved
	for i, e := range edits {
		start := -1
		for j := 0; j+len(e.Old) <= len(fullSource); j++ {
			if fullSource[j:j+len(e.Old)] == e.Old {
				start = j
				break
			}
			if j+len(e.Old) > len(fullSource) {
				break
			}
		}
		if start < 0 {
			return "", "", errors.New(fmt.Sprintf("text edit %d preimage not found in original source", i+1))
		}
		count := 0
		for j := 0; j+len(e.Old) <= len(fullSource); j++ {
			if fullSource[j:j+len(e.Old)] == e.Old {
				count++
			}
		}
		if count != 1 {
			return "", "", errors.New(fmt.Sprintf("text edit %d preimage matched %d times; expected exactly once; use an exact globally unique preimage within one authorized region", i+1, count))
		}
		end := start + len(e.Old)
		if err := validateUTF8Boundaries(fullSource, start, end); err != nil {
			return "", "", err
		}
		viewIndex := -1
		for vi, v := range views {
			if v.Start <= start && end <= v.End {
				viewIndex = vi
				break
			}
		}
		if viewIndex < 0 {
			return "", "", errors.New(fmt.Sprintf("text edit %d range not contained in a single bounded view", i+1))
		}
		res = append(res, resolved{start: start, end: end, viewIndex: viewIndex})
	}
	for i := 0; i < len(res); i++ {
		for j := i + 1; j < len(res); j++ {
			if res[i].start < res[j].end && res[j].start < res[i].end {
				return "", "", errors.New("overlapping edit intervals")
			}
		}
	}
	order := make([]int, len(res))
	for i := range order {
		order[i] = i
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if res[order[j]].start > res[order[i]].start {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	result := fullSource
	for _, idx := range order {
		result = result[:res[idx].start] + edits[idx].New + result[res[idx].end:]
	}
	return result, "replace_text", nil
}
