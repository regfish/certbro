// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriterProgressReporterAnimatesWaitOutput(t *testing.T) {
	var buf bytes.Buffer
	reporter := &writerProgressReporter{
		out:         &buf,
		interactive: true,
		width:       200,
	}

	reporter.WaitStart("certificate_id %s status=pending order_state=PENDING, still processing and this can take a few minutes", "ABC123")
	time.Sleep(350 * time.Millisecond)
	reporter.WaitDone("certificate_id %s is ready, downloading certificate", "ABC123")

	output := buf.String()
	if count := strings.Count(output, "[wait "); count < 2 {
		t.Fatalf("wait output count = %d, want at least 2 updates; output=%q", count, output)
	}
	if !strings.Contains(output, "this can take a few minutes") {
		t.Fatalf("output = %q, want wait hint", output)
	}
	if !strings.Contains(output, "downloading certificate") {
		t.Fatalf("output = %q, want completion message", output)
	}
}

func TestWriterProgressReporterTruncatesInteractiveWaitLines(t *testing.T) {
	var buf bytes.Buffer
	reporter := &writerProgressReporter{
		out:         &buf,
		interactive: true,
		width:       60,
	}

	reporter.WaitStart("certificate_id %s status=pending order_state=PENDING, still processing and this can take a few minutes", "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	reporter.WaitDone("done")

	output := buf.String()
	if !strings.Contains(output, "…") {
		t.Fatalf("output = %q, want ellipsis for truncated wait line", output)
	}
}
