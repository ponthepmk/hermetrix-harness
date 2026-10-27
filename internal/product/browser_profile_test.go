package product

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserProfileCleanupPreservesDirectoriesOutsideItsScope(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, test := range []struct{ name, path string }{
		{"data_root_itself", root},
		{"user_directory", filepath.Join(root, "project")},
		{"outside_root", filepath.Join(outside, "chrome-profile-owned-elsewhere")},
		{"nested_directory", filepath.Join(root, "project", "chrome-profile-nested")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.MkdirAll(test.path, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(test.path, "keep.txt")
			if err := os.WriteFile(file, []byte("owner data"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := removeBrowserProfile(root, test.path); err == nil {
				t.Fatal("unsafe profile removal was accepted")
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "owner data" {
				t.Fatalf("owner data changed: %q err=%v", data, err)
			}
		})
	}
	profile, err := os.MkdirTemp(root, "chrome-profile-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(profile, "temporary"), []byte("Chrome data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = removeBrowserProfile(root, profile); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned profile remains: %v", err)
	}
	if err = removeBrowserProfile(root, profile); err != nil {
		t.Fatalf("already removed profile: %v", err)
	}
	for _, file := range []string{filepath.Join(root, "keep.txt"), filepath.Join(root, "project", "keep.txt"), filepath.Join(outside, "chrome-profile-owned-elsewhere", "keep.txt")} {
		if data, err := os.ReadFile(file); err != nil || string(data) != "owner data" {
			t.Fatalf("valid cleanup changed unrelated data: %s %q err=%v", file, data, err)
		}
	}
}

func TestManagedBrowserProfileDisappearsBeforeCloseReturns(t *testing.T) {
	if _, err := chromeExecutable(); err != nil {
		if os.Getenv("HERMETRIX_REQUIRE_BROWSER_E2E") == "1" {
			t.Fatal(err)
		}
		t.Skip("Chrome unavailable")
	}
	s, _, _ := testProductService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		browser, err := s.ensureBrowser(ctx)
		if err != nil {
			t.Fatal(err)
		}
		profile := browser.profile
		s.closeBrowser()
		if !browser.command.ProcessState.Exited() {
			t.Fatal("Chrome root did not exit")
		}
		if _, err = os.Stat(profile); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Chrome profile remains after Close, round=%d path=%s err=%v", i, profile, err)
		}
	}
}
