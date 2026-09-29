package product

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func availableBrowseRoots() []DirectoryEntry {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	roots := make([]DirectoryEntry, 0, 4)
	for index := 0; index < 26; index++ {
		if mask&(1<<index) == 0 {
			continue
		}
		path := fmt.Sprintf("%c:\\", 'A'+index)
		roots = append(roots, DirectoryEntry{Name: path, Path: path})
	}
	return roots
}
