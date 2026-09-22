//go:build !windows

package product

import "path/filepath"

func localFileURLPath(urlPath string) string { return filepath.FromSlash(urlPath) }
