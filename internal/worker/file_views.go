// Package worker provides a proposal-only external model boundary. It never
// reads a workspace, executes commands, or applies model-generated changes.
package worker

import (
	"encoding/json"
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// BoundedFileView is a read-only bounded view of a supplied file source.
// It carries no authority and is not a filesystem or transport claim.
type BoundedFileView struct {
	Path       string `json:"path"`
	FullSHA256 string `json:"full_sha256"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Content    string `json:"content"`
	RegionID   string `json:"region_id"`
}

// NewBoundedFileView builds a bounded view from an independent expected path,
// full source, expected full SHA-256, and inclusive-exclusive byte range.
func NewBoundedFileView(path, fullSource, expectedFullSHA string, start, end int) (BoundedFileView, error) {
	if err := validatePath(path); err != nil {
		return BoundedFileView{}, err
	}
	if !utf8.ValidString(fullSource) {
		return BoundedFileView{}, errors.New("invalid UTF-8 source")
	}
	if expectedFullSHA != Hash(fullSource) {
		return BoundedFileView{}, errors.New("hash mismatch")
	}
	if start < 0 || start > end || end > len(fullSource) {
		return BoundedFileView{}, errors.New("invalid range")
	}
	if err := validateUTF8Boundaries(fullSource, start, end); err != nil {
		return BoundedFileView{}, err
	}
	view := BoundedFileView{
		Path:       path,
		FullSHA256: expectedFullSHA,
		Start:      start,
		End:        end,
		Content:    fullSource[start:end],
	}
	view.RegionID = hashRegionID(path, expectedFullSHA, start, end)
	return view, nil
}

// ValidateBoundedFileView validates a bounded view against independent expected
// path, source, and hash inputs, then compares all fields for tampering.
func ValidateBoundedFileView(expectedPath, fullSource, expectedFullSHA string, view BoundedFileView) error {
	if err := validatePath(expectedPath); err != nil {
		return err
	}
	if !utf8.ValidString(fullSource) {
		return errors.New("invalid UTF-8 source")
	}
	if expectedFullSHA != Hash(fullSource) {
		return errors.New("hash mismatch")
	}
	if view.Start < 0 || view.Start > view.End || view.End > len(fullSource) {
		return errors.New("invalid range")
	}
	if err := validateUTF8Boundaries(fullSource, view.Start, view.End); err != nil {
		return err
	}
	if view.Path != expectedPath || view.FullSHA256 != expectedFullSHA ||
		view.Content != fullSource[view.Start:view.End] {
		return errors.New("view mismatch")
	}
	if view.RegionID != hashRegionID(expectedPath, expectedFullSHA, view.Start, view.End) {
		return errors.New("region id mismatch")
	}
	return nil
}

func validatePath(p string) error {
	if p == "" || p == "." || p == ".." || path.IsAbs(p) {
		return errors.New("invalid path")
	}
	if strings.ContainsAny(p, `\:`) || !utf8.ValidString(p) {
		return errors.New("invalid path")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return errors.New("invalid path")
		}
	}
	if path.Clean(p) != p {
		return errors.New("invalid path")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New("invalid path")
		}
	}
	return nil
}

func validateUTF8Boundaries(source string, start, end int) error {
	if start < 0 || start > len(source) || end < 0 || end > len(source) {
		return errors.New("invalid UTF-8 boundary")
	}
	if !utf8.ValidString(source[:start]) || !utf8.ValidString(source[start:]) {
		return errors.New("invalid UTF-8 boundary")
	}
	if !utf8.ValidString(source[:end]) || !utf8.ValidString(source[end:]) {
		return errors.New("invalid UTF-8 boundary")
	}
	if strings.HasSuffix(source[:start], "\r") && strings.HasPrefix(source[start:], "\n") {
		return errors.New("invalid UTF-8 boundary")
	}
	if strings.HasSuffix(source[:end], "\r") && strings.HasPrefix(source[end:], "\n") {
		return errors.New("invalid UTF-8 boundary")
	}
	return nil
}

func hashRegionID(path, sha string, start, end int) string {
	var t struct {
		Path  string `json:"path"`
		SHA   string `json:"sha"`
		Start int    `json:"start"`
		End   int    `json:"end"`
	}
	t.Path, t.SHA, t.Start, t.End = path, sha, start, end
	b, _ := json.Marshal(t)
	return Hash(string(b))
}
