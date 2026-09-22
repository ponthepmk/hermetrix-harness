//go:build !windows

package product

import "os"

func platformCommandEnvironment(tempDir string) ([]string, map[string]bool) {
	values := []string{"TMPDIR=" + tempDir}
	found := map[string]bool{"TMPDIR": true}
	for _, key := range []string{"PATH", "LANG", "LC_ALL", "TERM"} {
		if value := os.Getenv(key); value != "" {
			values = append(values, key+"="+value)
			found[key] = true
		}
	}
	return values, found
}
