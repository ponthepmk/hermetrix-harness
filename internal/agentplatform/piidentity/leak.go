package piidentity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// ScanForLeak checks the source/output tree and local data root without ever
// emitting the credential or its digest. Pi's own credential digest database
// is outside this local output scan by design.
func (c *MCPCaller) ScanForLeak(workspace, dataRoot string, receipt []byte) error {
	hash := sha256.Sum256([]byte(c.token))
	patterns := [][]byte{[]byte(c.token), []byte(hex.EncodeToString(hash[:]))}
	check := func(body []byte) error {
		for _, pattern := range patterns {
			if bytes.Contains(body, pattern) {
				return errors.New("H2A credential material found in local output")
			}
		}
		return nil
	}
	if err := check(receipt); err != nil {
		return err
	}
	for _, root := range []string{workspace, dataRoot} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".cache", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return check(body)
		}); err != nil {
			return errors.New("H2A local secret-leak scan failed")
		}
	}
	return nil
}
