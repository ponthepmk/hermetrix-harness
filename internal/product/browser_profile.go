package product

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) cleanupBrowserProfile(profile string) {
	if err := removeBrowserProfile(s.store.Root, profile); err != nil {
		slog.Warn("Chrome profile cleanup failed; directory retained", "profile", profile, "error", err)
	}
}

// removeBrowserProfile removes only a generated direct child of this data root.
// Windows can briefly retain Crashpad mappings after Chrome has exited. Retry
// only those filesystem errors, for at most two seconds after the first attempt.
func removeBrowserProfile(dataRoot, profile string) error {
	root, err := filepath.Abs(dataRoot)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(profile)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) || filepath.Dir(relative) != "." || !strings.HasPrefix(relative, "chrome-profile-") {
		return fmt.Errorf("browser profile must be a generated direct child of the data root")
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return err
	}
	profileInfo, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !profileInfo.IsDir() || profileInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("browser profile is not a regular directory")
	}
	deadline := time.Now().Add(2 * time.Second)
	delay := time.Millisecond
	for {
		// Revalidate identities before every recursive removal. Another path or
		// replacement directory must never inherit a pending cleanup retry.
		parent, err := os.Stat(filepath.Dir(target))
		if err != nil {
			return err
		}
		if !os.SameFile(rootInfo, parent) {
			return fmt.Errorf("browser profile data root changed before cleanup")
		}
		current, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !os.SameFile(profileInfo, current) || !current.IsDir() || current.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("browser profile changed before cleanup")
		}
		err = os.RemoveAll(target)
		if !retryableBrowserProfileError(err) || !time.Now().Add(delay).Before(deadline) {
			return err
		}
		time.Sleep(delay)
		delay = min(2*delay, 50*time.Millisecond)
	}
}
