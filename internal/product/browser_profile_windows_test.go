//go:build windows

package product

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestBrowserProfileCleanupWaitsForAnActualWindowsFileLock(t *testing.T) {
	root := t.TempDir()
	profile, err := os.MkdirTemp(root, "chrome-profile-")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(profile, "CrashpadMetrics-active.pma")
	if err = os.WriteFile(file, []byte("mapped metrics fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(file)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Match the actual Chrome failure with a real OS lock, not a mocked retry.
	if err = os.RemoveAll(profile); !retryableBrowserProfileError(err) {
		_ = windows.CloseHandle(handle)
		t.Fatalf("fixture did not hold a retryable Windows lock: %v", err)
	}
	released := make(chan error, 1)
	go func() { time.Sleep(30 * time.Millisecond); released <- windows.CloseHandle(handle) }()
	defer func() {
		if err := <-released; err != nil {
			t.Error(err)
		}
	}()
	if err = removeBrowserProfile(root, profile); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("locked profile remains: %v", err)
	}
}
