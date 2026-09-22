//go:build windows

package store

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type dataRootLock struct{ file *os.File }

func acquireDataRootLock(path string) (*dataRootLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("data root is already owned by another Hermetrix process: %w", err)
	}
	return &dataRootLock{file: file}, nil
}

func (l *dataRootLock) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	overlapped := new(windows.Overlapped)
	unlockErr := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
