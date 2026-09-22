//go:build !windows

package store

import (
	"fmt"
	"os"
	"syscall"
)

type dataRootLock struct{ file *os.File }

func acquireDataRootLock(path string) (*dataRootLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
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
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
