//go:build windows

package product

import "os"

func platformCommandEnvironment(tempDir string) ([]string, map[string]bool) {
	values := []string{"TEMP=" + tempDir, "TMP=" + tempDir}
	found := map[string]bool{"TEMP": true, "TMP": true}
	for _, key := range []string{"PATH", "SystemRoot", "PATHEXT"} {
		if value := os.Getenv(key); value != "" {
			values = append(values, key+"="+value)
			found[key] = true
		}
	}
	return values, found
}
