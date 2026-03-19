// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type progressReporter interface {
	Stepf(format string, args ...any)
	WaitStart(format string, args ...any)
	WaitTick(format string, args ...any)
	WaitDone(format string, args ...any)
}

type nopProgressReporter struct{}

func (nopProgressReporter) Stepf(string, ...any)     {}
func (nopProgressReporter) WaitStart(string, ...any) {}
func (nopProgressReporter) WaitTick(string, ...any)  {}
func (nopProgressReporter) WaitDone(string, ...any)  {}

type writerProgressReporter struct {
	mu            sync.Mutex
	out           io.Writer
	interactive   bool
	width         int
	waiting       bool
	lastWaitMsg   string
	lastWaitFrame int
	lastRenderLen int
	waitToken     uint64
}

func newWriterProgressReporter(out io.Writer) progressReporter {
	if out == nil {
		return nopProgressReporter{}
	}
	reporter := &writerProgressReporter{
		out:         out,
		interactive: progressWriterIsInteractive(out),
		width:       progressWriterWidth(),
	}
	return reporter
}

func (r *writerProgressReporter) Stepf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.finishWaitLocked()
	fmt.Fprintf(r.out, "==> %s\n", fmt.Sprintf(format, args...))
}

func (r *writerProgressReporter) WaitStart(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if !r.interactive {
		r.waiting = true
		r.lastWaitMsg = msg
		fmt.Fprintf(r.out, "==> %s\n", msg)
		return
	}

	r.finishWaitLocked()
	r.waiting = true
	r.lastWaitMsg = msg
	r.lastWaitFrame = 0
	r.renderCurrentLocked()
	r.startAnimationLocked()
}

func (r *writerProgressReporter) WaitTick(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if !r.interactive {
		if !r.waiting || msg != r.lastWaitMsg {
			fmt.Fprintf(r.out, "==> %s\n", msg)
		}
		r.waiting = true
		r.lastWaitMsg = msg
		return
	}

	if !r.waiting {
		r.waiting = true
		r.lastWaitFrame = 0
		r.startAnimationLocked()
	}
	r.lastWaitMsg = msg
	r.renderCurrentLocked()
}

func (r *writerProgressReporter) WaitDone(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if !r.interactive {
		r.waiting = false
		r.lastWaitMsg = ""
		r.lastWaitFrame = 0
		r.lastRenderLen = 0
		fmt.Fprintf(r.out, "==> %s\n", msg)
		return
	}

	if r.waiting {
		r.clearCurrentLocked()
		r.waiting = false
		r.lastWaitMsg = ""
		r.lastWaitFrame = 0
		r.lastRenderLen = 0
		r.waitToken++
	}
	fmt.Fprintf(r.out, "==> %s\n", msg)
}

func (r *writerProgressReporter) finishWaitLocked() {
	if !r.waiting {
		return
	}
	r.clearCurrentLocked()
	r.waiting = false
	r.lastWaitMsg = ""
	r.lastWaitFrame = 0
	r.lastRenderLen = 0
	r.waitToken++
}

func (r *writerProgressReporter) renderWaitLocked() string {
	frames := []string{"-", "\\", "|", "/"}
	frame := frames[r.lastWaitFrame%len(frames)]
	now := time.Now().UTC().Format("15:04:05")
	line := fmt.Sprintf("[wait %s] %s %s", now, frame, r.lastWaitMsg)
	return truncateProgressLine(line, r.width)
}

func (r *writerProgressReporter) renderCurrentLocked() {
	line := r.renderWaitLocked()
	r.lastRenderLen = len(line)
	fmt.Fprintf(r.out, "\r\033[2K%s", line)
}

func (r *writerProgressReporter) clearCurrentLocked() {
	if r.lastRenderLen == 0 {
		return
	}
	fmt.Fprint(r.out, "\r\033[2K")
}

func (r *writerProgressReporter) startAnimationLocked() {
	if !r.interactive {
		return
	}
	r.waitToken++
	token := r.waitToken
	go func() {
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			r.mu.Lock()
			if !r.waiting || r.waitToken != token {
				r.mu.Unlock()
				return
			}
			r.lastWaitFrame++
			r.renderCurrentLocked()
			r.mu.Unlock()
		}
	}()
}

func progressWriterIsInteractive(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func progressWriterWidth() int {
	columns := strings.TrimSpace(os.Getenv("COLUMNS"))
	if columns == "" {
		return 100
	}
	width, err := strconv.Atoi(columns)
	if err != nil || width < 20 {
		return 100
	}
	return width
}

func truncateProgressLine(line string, width int) string {
	if width <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) < width {
		return line
	}
	if width <= 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + "…"
}
