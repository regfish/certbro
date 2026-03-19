//go:build windows

// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package lock

import "errors"

// ErrLocked reports that another certbro renew process already holds the lock.
var ErrLocked = errors.New("lock already held")

// FileLock is a no-op placeholder on Windows.
type FileLock struct{}

// Acquire returns a no-op lock on Windows where certbro does not use flock.
func Acquire(path string) (*FileLock, error) {
	return &FileLock{}, nil
}

// Close is a no-op on Windows.
func (l *FileLock) Close() error {
	return nil
}
