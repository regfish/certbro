// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package deploy

import "testing"

func TestWebserverPlanNginx(t *testing.T) {
	plan, err := webserverPlan(WebserverIntegration{Kind: "nginx"})
	if err != nil {
		t.Fatalf("webserverPlan() error = %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("len(plan) = %d, want 2", len(plan))
	}
	if got := formatCommand(plan[0].Choices[0]); got != "nginx -t" {
		t.Fatalf("validate command = %q", got)
	}
	if got := formatCommand(plan[1].Choices[0]); got != "systemctl reload nginx" {
		t.Fatalf("reload command = %q", got)
	}
}

func TestWebserverPlanApacheCustomConfig(t *testing.T) {
	plan, err := webserverPlan(WebserverIntegration{Kind: "apache", ConfigPath: "/etc/apache2/apache2.conf"})
	if err != nil {
		t.Fatalf("webserverPlan() error = %v", err)
	}
	if got := formatCommand(plan[0].Choices[0]); got != "apachectl -f /etc/apache2/apache2.conf -t" {
		t.Fatalf("validate command = %q", got)
	}
	if got := formatCommand(plan[1].Choices[0]); got != "apachectl -f /etc/apache2/apache2.conf -k graceful" {
		t.Fatalf("reload command = %q", got)
	}
}

func TestWebserverPlanCaddyDefaultsConfigPath(t *testing.T) {
	plan, err := webserverPlan(WebserverIntegration{Kind: "caddy"})
	if err != nil {
		t.Fatalf("webserverPlan() error = %v", err)
	}
	if got := formatCommand(plan[0].Choices[0]); got != "caddy validate --config /etc/caddy/Caddyfile" {
		t.Fatalf("validate command = %q", got)
	}
	if got := formatCommand(plan[1].Choices[0]); got != "caddy reload --config /etc/caddy/Caddyfile" {
		t.Fatalf("reload command = %q", got)
	}
}

func TestWebserverPlanRejectsUnsupportedKinds(t *testing.T) {
	if _, err := webserverPlan(WebserverIntegration{Kind: "iis"}); err == nil {
		t.Fatal("webserverPlan() error = nil, want error")
	}
}
