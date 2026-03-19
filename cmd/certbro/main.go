// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/regfish/certbro/internal/app"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// main wires signal handling into the application entrypoint.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.New(version, commit, buildDate).Run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "certbro:", err)
		os.Exit(1)
	}
}
