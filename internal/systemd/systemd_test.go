// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package systemd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderServiceAndTimer(t *testing.T) {
	opts := Options{
		ServiceName: "certbro",
		BinaryPath:  "/usr/local/bin/certbro",
		EnvFile:     "/etc/certbro/certbro.env",
		OnCalendar:  "daily",
	}

	service := RenderService(opts)
	timer := RenderTimer(opts)

	if !strings.Contains(service, "ExecStart=/usr/local/bin/certbro renew") {
		t.Fatalf("service missing ExecStart: %s", service)
	}
	if !strings.Contains(service, "EnvironmentFile=/etc/certbro/certbro.env") {
		t.Fatalf("service missing env file: %s", service)
	}
	if !strings.Contains(timer, "OnCalendar=daily") {
		t.Fatalf("timer missing schedule: %s", timer)
	}
}

func TestRenderEnvFileIncludesUserAgentMetadata(t *testing.T) {
	opts := Options{
		StateFile:       "/etc/certbro/state.json",
		CertificatesDir: "/etc/certbro",
		APIKey:          "secret",
		APIBaseURL:      "https://api.regfish.example",
		ContactEmail:    "ops@example.com",
	}

	envFile := renderEnvFile(opts)
	for _, needle := range []string{
		`CERTBRO_STATE_FILE="/etc/certbro/state.json"`,
		`CERTBRO_CERTIFICATES_DIR="/etc/certbro"`,
		`REGFISH_API_KEY="secret"`,
		`REGFISH_API_BASE="https://api.regfish.example"`,
		`CERTBRO_CONTACT_EMAIL="ops@example.com"`,
	} {
		if !strings.Contains(envFile, needle) {
			t.Fatalf("env file missing %q:\n%s", needle, envFile)
		}
	}
	if strings.Contains(envFile, "CERTBRO_USER_AGENT_INSTANCE=") {
		t.Fatalf("env file unexpectedly contains CERTBRO_USER_AGENT_INSTANCE:\n%s", envFile)
	}
}

func TestInstallDefaultsToHourlyTimer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Install() is Linux-only")
	}

	root := t.TempDir()
	systemdDir := filepath.Join(root, "systemd")
	stateFile := filepath.Join(root, "state.json")

	if err := Install(Options{
		SystemdDir:    systemdDir,
		StateFile:     stateFile,
		BinaryPath:    "/usr/local/bin/certbro",
		SkipSystemctl: true,
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	timerRaw, err := os.ReadFile(filepath.Join(systemdDir, "certbro.timer"))
	if err != nil {
		t.Fatalf("ReadFile(timer) error = %v", err)
	}
	if !strings.Contains(string(timerRaw), "OnCalendar=hourly") {
		t.Fatalf("timer missing hourly schedule: %s", string(timerRaw))
	}
}
