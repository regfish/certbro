//go:build !windows

// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package lock provides a best-effort process lock used by certbro renew.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked reports that another certbro renew process already holds the lock.
var ErrLocked = errors.New("lock already held")

// FileLock represents an acquired advisory file lock.
type FileLock struct {
	file *os.File
}

// Acquire opens and exclusively locks the given path.
func Acquire(path string) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory for %s: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}

	return &FileLock{file: file}, nil
}

// Close releases the file lock and closes the backing file.
func (l *FileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = l.file.Close()
		return err
	}
	return l.file.Close()
}
