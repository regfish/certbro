// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package deploy

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const (
	// WebserverNginx selects the built-in nginx validation and reload logic.
	WebserverNginx = "nginx"
	// WebserverApache selects the built-in Apache validation and reload logic.
	WebserverApache = "apache"
	// WebserverCaddy selects the built-in Caddy validation and reload logic.
	WebserverCaddy         = "caddy"
	defaultCaddyConfigPath = "/etc/caddy/Caddyfile"
)

// WebserverIntegration describes the post-deploy webserver reload target.
type WebserverIntegration struct {
	Kind       string
	ConfigPath string
}

type commandSpec struct {
	Name string
	Args []string
}

type commandGroup struct {
	Label   string
	Choices []commandSpec
}

// ReloadWebserver validates and reloads the configured webserver integration.
func ReloadWebserver(integration WebserverIntegration) error {
	integration.Kind = normalizeWebserverKind(integration.Kind)
	if integration.Kind == "" {
		return nil
	}

	plan, err := webserverPlan(integration)
	if err != nil {
		return err
	}
	for _, group := range plan {
		if err := runCommandGroup(group); err != nil {
			return err
		}
	}
	return nil
}

// ValidateWebserverIntegration verifies that a webserver integration kind is supported.
func ValidateWebserverIntegration(integration WebserverIntegration) error {
	integration.Kind = normalizeWebserverKind(integration.Kind)
	if integration.Kind == "" {
		return nil
	}
	_, err := webserverPlan(integration)
	return err
}

func normalizeWebserverKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "", WebserverNginx, WebserverApache, WebserverCaddy:
		return kind
	case "httpd":
		return WebserverApache
	}
	return kind
}

func webserverPlan(integration WebserverIntegration) ([]commandGroup, error) {
	switch integration.Kind {
	case WebserverNginx:
		return nginxPlan(strings.TrimSpace(integration.ConfigPath)), nil
	case WebserverApache:
		return apachePlan(strings.TrimSpace(integration.ConfigPath)), nil
	case WebserverCaddy:
		return caddyPlan(strings.TrimSpace(integration.ConfigPath)), nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported webserver %q", integration.Kind)
	}
}

func nginxPlan(configPath string) []commandGroup {
	validate := commandSpec{Name: "nginx", Args: []string{"-t"}}
	reload := commandSpec{Name: "nginx", Args: []string{"-s", "reload"}}
	if configPath != "" {
		validate.Args = []string{"-c", configPath, "-t"}
		reload.Args = []string{"-c", configPath, "-s", "reload"}
	}
	return []commandGroup{
		{
			Label:   "validate nginx configuration",
			Choices: []commandSpec{validate},
		},
		{
			Label: "reload nginx",
			Choices: []commandSpec{
				{Name: "systemctl", Args: []string{"reload", "nginx"}},
				reload,
			},
		},
	}
}

func apachePlan(configPath string) []commandGroup {
	validateArgs := []string{"-t"}
	reloadArgs := []string{"-k", "graceful"}
	if configPath != "" {
		validateArgs = []string{"-f", configPath, "-t"}
		reloadArgs = []string{"-f", configPath, "-k", "graceful"}
	}
	return []commandGroup{
		{
			Label: "validate apache configuration",
			Choices: []commandSpec{
				{Name: "apachectl", Args: validateArgs},
				{Name: "apache2ctl", Args: validateArgs},
			},
		},
		{
			Label: "reload apache",
			Choices: []commandSpec{
				{Name: "apachectl", Args: reloadArgs},
				{Name: "apache2ctl", Args: reloadArgs},
				{Name: "systemctl", Args: []string{"reload", "apache2"}},
				{Name: "systemctl", Args: []string{"reload", "httpd"}},
			},
		},
	}
}

func caddyPlan(configPath string) []commandGroup {
	if configPath == "" {
		configPath = defaultCaddyConfigPath
	}
	return []commandGroup{
		{
			Label: "validate caddy configuration",
			Choices: []commandSpec{
				{Name: "caddy", Args: []string{"validate", "--config", configPath}},
			},
		},
		{
			Label: "reload caddy",
			Choices: []commandSpec{
				{Name: "caddy", Args: []string{"reload", "--config", configPath}},
				{Name: "systemctl", Args: []string{"reload", "caddy"}},
			},
		},
	}
}

func runCommandGroup(group commandGroup) error {
	var attempted []string
	var lastErr error
	for _, choice := range group.Choices {
		if _, err := exec.LookPath(choice.Name); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				continue
			}
			lastErr = err
			continue
		}
		attempted = append(attempted, formatCommand(choice))
		cmd := exec.Command(choice.Name, choice.Args...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		output, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("%s: %w\n%s", formatCommand(choice), err, strings.TrimSpace(string(output)))
	}
	if len(attempted) == 0 {
		return fmt.Errorf("%s: no usable command found", group.Label)
	}
	return fmt.Errorf("%s: %w", group.Label, lastErr)
}

func formatCommand(command commandSpec) string {
	if len(command.Args) == 0 {
		return command.Name
	}
	return command.Name + " " + strings.Join(command.Args, " ")
}
