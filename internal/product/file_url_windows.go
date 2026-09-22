//go:build windows

package product

import "path/filepath"

func localFileURLPath(urlPath string) string {
	path := filepath.FromSlash(urlPath)
	if len(path) >= 3 && (path[0] == '\\' || path[0] == '/') && path[2] == ':' &&
		((path[1] >= 'a' && path[1] <= 'z') || (path[1] >= 'A' && path[1] <= 'Z')) {
		path = path[1:]
	}
	return path
}
