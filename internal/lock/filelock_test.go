//go:build !windows

// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package lock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquirePreventsConcurrentLocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certbro.lock")

	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire() first error = %v", err)
	}
	defer first.Close()

	second, err := Acquire(path)
	if !errors.Is(err, ErrLocked) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("Acquire() second error = %v, want ErrLocked", err)
	}
}
