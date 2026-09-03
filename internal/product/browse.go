package product

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registering a project needs an absolute path, and a browser will not give one
// up: a file input reports names, never locations, on purpose. So the directory
// walk happens here, on the machine that owns the filesystem, and the UI shows
// what this returns.
//
// This is the one place Hermetrix lists a directory that is not already inside
// a registered project, which is unavoidable -- you cannot pick a root from
// inside the roots you have already picked. It is bounded to match: directory
// names only, never file contents, never sizes, never anything from a file at
// all, and only over a loopback listener.

// maxBrowseEntries caps one listing. A home directory with tens of thousands of
// entries should answer slowly-but-bounded rather than building a response
// nobody can read.
const maxBrowseEntries = 500

// DirectoryEntry is one child directory offered for selection.
type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirectoryListing is one level of the picker.
type DirectoryListing struct {
	Path string `json:"path"`
	// Parent is empty at the filesystem root, which is how the UI knows to stop
	// offering "up".
	Parent      string           `json:"parent"`
	Home        string           `json:"home"`
	Entries     []DirectoryEntry `json:"entries"`
	Truncated   bool             `json:"truncated"`
	Unreadable  bool             `json:"unreadable"`
	AlreadyRoot bool             `json:"already_root"`
}

// BrowseDirectories lists the directories under path. An empty path starts at
// the user's home directory, which is where somebody looking for their code is
// most likely to start.
func (s *Service) BrowseDirectories(path string) (DirectoryListing, error) {
	home, _ := os.UserHomeDir()
	target := strings.TrimSpace(path)
	if target == "" {
		target = home
	}
	if target == "" {
		target = string(filepath.Separator)
	}
	if !filepath.IsAbs(target) {
		return DirectoryListing{}, fmt.Errorf("a directory to browse must be an absolute path")
	}
	resolved, err := resolveProjectRoot(target)
	if err != nil {
		return DirectoryListing{}, err
	}
	listing := DirectoryListing{Path: resolved, Home: home, Entries: []DirectoryEntry{}}
	if parent := filepath.Dir(resolved); parent != resolved {
		listing.Parent = parent
	}
	items, err := os.ReadDir(resolved)
	if err != nil {
		// A directory you cannot read is a normal thing to walk past, not a
		// failure of the picker: say so and let the user go back up.
		listing.Unreadable = true
		return listing, nil
	}
	for _, item := range items {
		if len(listing.Entries) >= maxBrowseEntries {
			listing.Truncated = true
			break
		}
		name := item.Name()
		child := filepath.Join(resolved, name)
		if item.IsDir() {
			listing.Entries = append(listing.Entries, DirectoryEntry{Name: name, Path: child})
			continue
		}
		// A symlink may still point at a directory, and a project root behind
		// one is ordinary. Follow it only far enough to answer that question.
		if item.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(child); err == nil && info.IsDir() {
				listing.Entries = append(listing.Entries, DirectoryEntry{Name: name, Path: child})
			}
		}
	}
	sort.Slice(listing.Entries, func(a, b int) bool {
		left, right := listing.Entries[a], listing.Entries[b]
		// Hidden directories sort last: they are real choices, but they are not
		// what someone is looking for when they open a picker.
		leftHidden := strings.HasPrefix(left.Name, ".")
		rightHidden := strings.HasPrefix(right.Name, ".")
		if leftHidden != rightHidden {
			return rightHidden
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	return listing, nil
}
