// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package systemd renders and installs Linux systemd units for certbro renewals.
package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Options controls the generated systemd unit and environment files.
type Options struct {
	ServiceName     string
	BinaryPath      string
	SystemdDir      string
	EnvFile         string
	OnCalendar      string
	StateFile       string
	CertificatesDir string
	APIKey          string
	APIBaseURL      string
	ContactEmail    string
	SkipSystemctl   bool
}

// Install writes the certbro systemd units and optionally enables the timer.
func Install(opts Options) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("systemd installation is only supported on Linux")
	}

	if strings.TrimSpace(opts.ServiceName) == "" {
		opts.ServiceName = "certbro"
	}
	if strings.TrimSpace(opts.SystemdDir) == "" {
		opts.SystemdDir = "/etc/systemd/system"
	}
	if strings.TrimSpace(opts.OnCalendar) == "" {
		opts.OnCalendar = "hourly"
	}
	if strings.TrimSpace(opts.BinaryPath) == "" {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("determine binary path: %w", err)
		}
		opts.BinaryPath = executable
	}
	if strings.TrimSpace(opts.EnvFile) == "" {
		baseDir := filepath.Dir(opts.StateFile)
		if strings.TrimSpace(opts.CertificatesDir) != "" {
			baseDir = opts.CertificatesDir
		}
		opts.EnvFile = filepath.Join(baseDir, "certbro.env")
	}

	if err := os.MkdirAll(filepath.Dir(opts.EnvFile), 0o755); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}
	if err := os.MkdirAll(opts.SystemdDir, 0o755); err != nil {
		return fmt.Errorf("create systemd dir: %w", err)
	}

	if err := os.WriteFile(opts.EnvFile, []byte(renderEnvFile(opts)), 0o600); err != nil {
		return fmt.Errorf("write env file %s: %w", opts.EnvFile, err)
	}

	servicePath := filepath.Join(opts.SystemdDir, opts.ServiceName+".service")
	timerPath := filepath.Join(opts.SystemdDir, opts.ServiceName+".timer")
	if err := os.WriteFile(servicePath, []byte(RenderService(opts)), 0o644); err != nil {
		return fmt.Errorf("write service unit %s: %w", servicePath, err)
	}
	if err := os.WriteFile(timerPath, []byte(RenderTimer(opts)), 0o644); err != nil {
		return fmt.Errorf("write timer unit %s: %w", timerPath, err)
	}

	if opts.SkipSystemctl {
		return nil
	}

	commands := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "--now", opts.ServiceName + ".timer"},
	}
	for _, argv := range commands {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("run %s: %w", strings.Join(argv, " "), err)
		}
	}

	return nil
}

// RenderService renders the certbro oneshot renewal service unit.
func RenderService(opts Options) string {
	return fmt.Sprintf(`[Unit]
Description=Renew regfish TLS certificates with certbro
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=%s
ExecStart=%s renew
`, opts.EnvFile, opts.BinaryPath)
}

// RenderTimer renders the periodic timer unit used to trigger certbro renew.
func RenderTimer(opts Options) string {
	return fmt.Sprintf(`[Unit]
Description=Run certbro renewal periodically

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=30m

[Install]
WantedBy=timers.target
`, opts.OnCalendar)
}

func renderEnvFile(opts Options) string {
	lines := []string{
		"CERTBRO_STATE_FILE=" + shellQuoteEnv(opts.StateFile),
	}
	if strings.TrimSpace(opts.CertificatesDir) != "" {
		lines = append(lines, "CERTBRO_CERTIFICATES_DIR="+shellQuoteEnv(opts.CertificatesDir))
	}
	if strings.TrimSpace(opts.APIKey) != "" {
		lines = append(lines, "REGFISH_API_KEY="+shellQuoteEnv(opts.APIKey))
	}
	if strings.TrimSpace(opts.APIBaseURL) != "" {
		lines = append(lines, "REGFISH_API_BASE="+shellQuoteEnv(opts.APIBaseURL))
	}
	if strings.TrimSpace(opts.ContactEmail) != "" {
		lines = append(lines, "CERTBRO_CONTACT_EMAIL="+shellQuoteEnv(opts.ContactEmail))
	}
	return strings.Join(lines, "\n") + "\n"
}

func shellQuoteEnv(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return `"` + value + `"`
}
