package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrInvalidRef = errors.New("invalid blob reference")

// Store is a content-addressed local blob store. A successful Put is durable
// before its reference can be committed into SQLite.
type Store struct {
	root string
}

type Info struct {
	Ref   string `json:"ref"`
	Bytes int64  `json:"bytes"`
}

func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("blob root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create blob root: %w", err)
	}
	return &Store{root: root}, nil
}

func (s *Store) Put(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	ref := hex.EncodeToString(sum[:])
	path, err := s.path(ref)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return ref, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("stat blob: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create blob shard: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hermetrix-blob-*")
	if err != nil {
		return "", fmt.Errorf("create temporary blob: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", fmt.Errorf("chmod temporary blob: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write temporary blob: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("sync temporary blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close temporary blob: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return ref, nil
		}
		return "", fmt.Errorf("commit blob: %w", err)
	}
	return ref, nil
}

func (s *Store) Get(ref string) ([]byte, error) {
	path, err := s.path(ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", ref, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != ref {
		return nil, fmt.Errorf("blob %s failed integrity check", ref)
	}
	return data, nil
}

func (s *Store) Exists(ref string) bool {
	path, err := s.path(ref)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func (s *Store) List() ([]Info, error) {
	items := []Info{}
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 2 {
			return nil
		}
		ref := parts[0] + parts[1]
		if _, err := s.path(ref); err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		items = append(items, Info{Ref: ref, Bytes: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list blobs: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	return items, nil
}

// Quarantine moves a blob out of the active CAS without deleting it. The
// caller must have produced and approved an exact GC snapshot first.
func (s *Store) Quarantine(ref, destinationRoot string) (string, error) {
	source, err := s.path(ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(destinationRoot) == "" {
		return "", fmt.Errorf("quarantine destination is empty")
	}
	destination := filepath.Join(destinationRoot, ref[:2], ref[2:])
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(source, destination); err != nil {
		return "", fmt.Errorf("quarantine blob %s: %w", ref, err)
	}
	return destination, nil
}

// RestoreFromQuarantine verifies the quarantined bytes before moving them back
// into the active CAS. It never overwrites a different active object.
func (s *Store) RestoreFromQuarantine(ref, sourceRoot string) (string, error) {
	destination, err := s.path(ref)
	if err != nil {
		return "", err
	}
	if data, readErr := os.ReadFile(destination); readErr == nil {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != ref {
			return "", fmt.Errorf("active blob %s failed integrity check", ref)
		}
		return destination, nil
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return "", readErr
	}
	source := filepath.Join(sourceRoot, ref[:2], ref[2:])
	data, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read quarantined blob %s: %w", ref, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != ref {
		return "", fmt.Errorf("quarantined blob %s failed integrity check", ref)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(source, destination); err != nil {
		return "", fmt.Errorf("restore quarantined blob %s: %w", ref, err)
	}
	return destination, nil
}

func (s *Store) path(ref string) (string, error) {
	if len(ref) != 64 {
		return "", ErrInvalidRef
	}
	if _, err := hex.DecodeString(ref); err != nil {
		return "", ErrInvalidRef
	}
	return filepath.Join(s.root, ref[:2], ref[2:]), nil
}
