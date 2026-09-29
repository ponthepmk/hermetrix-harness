//go:build !windows

package product

func availableBrowseRoots() []DirectoryEntry {
	return []DirectoryEntry{{Name: "/", Path: "/"}}
}
