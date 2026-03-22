// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/regfish/certbro/internal/config"
	certcrypto "github.com/regfish/certbro/internal/crypto"
	"github.com/regfish/certbro/internal/testutil"
)

func TestRunUpdateSetsValidityDays(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	outputDir := filepath.Join(root, "example.com")

	store := &config.Store{
		Version: config.CurrentVersion,
		ManagedCertificates: []config.ManagedCertificate{
			{
				Name:          "example-com",
				CommonName:    "example.com",
				OutputDir:     outputDir,
				ValidityDays:  199,
				CertificateID: "7K9QW3M2ZT8HJ",
			},
		},
	}
	if err := config.Save(statePath, store); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	app := &App{}
	if err := app.runUpdate([]string{"--name", "example-com", "--validity-days", "90"}, rootOptions{StateFile: statePath}, store); err != nil {
		t.Fatalf("runUpdate() error = %v", err)
	}

	updatedStore, err := config.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	managed, _ := updatedStore.FindManagedCertificate("example-com")
	if managed == nil {
		t.Fatal("updated managed certificate not found")
	}
	if managed.ValidityDays != 90 {
		t.Fatalf("managed.ValidityDays = %d, want 90", managed.ValidityDays)
	}

	onDisk, err := config.LoadManagedCertificate(filepath.Join(outputDir, config.ManagedCertFileName))
	if err != nil {
		t.Fatalf("LoadManagedCertificate() error = %v", err)
	}
	if onDisk.ValidityDays != 90 {
		t.Fatalf("onDisk.ValidityDays = %d, want 90", onDisk.ValidityDays)
	}
}

func TestRunUpdateRejectsValidityDaysAboveCurrentLimit(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	outputDir := filepath.Join(root, "example.com")

	store := &config.Store{
		Version: config.CurrentVersion,
		ManagedCertificates: []config.ManagedCertificate{
			{
				Name:       "example-com",
				CommonName: "example.com",
				OutputDir:  outputDir,
			},
		},
	}
	if err := config.Save(statePath, store); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	app := &App{}
	err := app.runUpdate([]string{"--name", "example-com", "--validity-days", "401"}, rootOptions{StateFile: statePath}, store)
	if err == nil {
		t.Fatal("runUpdate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("runUpdate() error = %v, want schedule-limit error", err)
	}
}

func TestRunUpdateRejectsValidityDaysNotGreaterThanRenewBeforeDays(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	outputDir := filepath.Join(root, "example.com")

	store := &config.Store{
		Version: config.CurrentVersion,
		ManagedCertificates: []config.ManagedCertificate{
			{
				Name:            "example-com",
				CommonName:      "example.com",
				OutputDir:       outputDir,
				ValidityDays:    30,
				RenewBeforeDays: 7,
				ReissueLeadDays: 7,
			},
		},
	}
	if err := config.Save(statePath, store); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	app := &App{}
	err := app.runUpdate([]string{"--name", "example-com", "--validity-days", "3"}, rootOptions{StateFile: statePath}, store)
	if err == nil {
		t.Fatal("runUpdate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--renew-before-days") {
		t.Fatalf("runUpdate() error = %v, want renewal-timing error", err)
	}
}

func TestBuildIssuePairManagedCertificates(t *testing.T) {
	rsaManaged, ecdsaManaged := buildIssuePairManagedCertificates(issuePairOptions{
		NameBase:        "example-com",
		CommonName:      "example.com",
		DNSNames:        []string{"www.example.com", "api.example.com"},
		Product:         "RapidSSL",
		OutputDirBase:   "/etc/certbro/example.com",
		OrganizationID:  7,
		ValidityDays:    90,
		RenewBeforeDays: 7,
		ReissueLeadDays: 7,
		RSABits:         3072,
		ECDSACurve:      "p384",
		Webserver:       "nginx",
		WebserverConfig: "/etc/nginx/nginx.conf",
		InstallHook:     "systemctl reload nginx",
	})

	if rsaManaged.Name != "example-com-rsa" || ecdsaManaged.Name != "example-com-ecdsa" {
		t.Fatalf("managed names = %q and %q", rsaManaged.Name, ecdsaManaged.Name)
	}
	if rsaManaged.OutputDir != "/etc/certbro/example.com-rsa" || ecdsaManaged.OutputDir != "/etc/certbro/example.com-ecdsa" {
		t.Fatalf("managed output dirs = %q and %q", rsaManaged.OutputDir, ecdsaManaged.OutputDir)
	}
	if rsaManaged.KeyType != certcrypto.KeyTypeRSA || rsaManaged.RSABits != 3072 {
		t.Fatalf("rsa managed = %#v", rsaManaged)
	}
	if ecdsaManaged.KeyType != certcrypto.KeyTypeECDSA || ecdsaManaged.ECDSACurve != "p384" {
		t.Fatalf("ecdsa managed = %#v", ecdsaManaged)
	}
	if rsaManaged.Webserver != "nginx" || ecdsaManaged.WebserverConfig != "/etc/nginx/nginx.conf" {
		t.Fatalf("webserver integration not propagated: %#v %#v", rsaManaged, ecdsaManaged)
	}
}

func TestRunIssueRejectsValidityDaysNotGreaterThanRenewBeforeDays(t *testing.T) {
	app := &App{}
	err := app.runIssue(context.Background(), []string{
		"--common-name", "example.com",
		"--output-dir", t.TempDir(),
		"--validity-days", "3",
		"--renew-before-days", "7",
		"--reissue-lead-days", "2",
	}, rootOptions{StateFile: filepath.Join(t.TempDir(), "state.json")}, &config.Store{Version: config.CurrentVersion})
	if err == nil {
		t.Fatal("runIssue() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--renew-before-days") {
		t.Fatalf("runIssue() error = %v, want renewal-timing error", err)
	}
}

func TestRunIssuePairRejectsExistingManagedCertificate(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	store := &config.Store{
		Version: config.CurrentVersion,
		ManagedCertificates: []config.ManagedCertificate{
			{
				Name:      "example-com-rsa",
				OutputDir: filepath.Join(root, "example.com-rsa"),
				Product:   "RapidSSL",
			},
		},
	}
	if err := config.Save(statePath, store); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	app := &App{}
	err := app.runIssuePair(context.Background(), []string{
		"--name-base", "example-com",
		"--common-name", "example.com",
		"--output-dir-base", filepath.Join(root, "example.com"),
	}, rootOptions{StateFile: statePath}, store)
	if err == nil {
		t.Fatal("runIssuePair() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `managed certificate "example-com-rsa" already exists`) {
		t.Fatalf("runIssuePair() error = %v", err)
	}
}

func TestRunConfigureValidatesAndPersistsAPIKey(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tls/certificate" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "valid-key" {
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"response":[]}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := &config.Store{Version: config.CurrentVersion}
	app := &App{Version: "test"}

	if err := app.runConfigure(context.Background(), []string{"--api-key", "valid-key", "--api-base-url", server.URL}, statePath, store); err != nil {
		t.Fatalf("runConfigure() error = %v", err)
	}
	if store.APIKey != "valid-key" {
		t.Fatalf("store.APIKey = %q, want valid-key", store.APIKey)
	}
	if store.APIKeyValidatedAt == nil {
		t.Fatal("store.APIKeyValidatedAt is nil, want verification timestamp")
	}

	onDisk, err := config.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if onDisk.APIKeyValidatedAt == nil {
		t.Fatal("persisted APIKeyValidatedAt is nil, want verification timestamp")
	}
}

func TestRunConfigureRejectsInvalidAPIKey(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "state.json")
	store := &config.Store{Version: config.CurrentVersion}
	app := &App{Version: "test"}

	err = app.runConfigure(context.Background(), []string{"--api-key", "wrong-key", "--api-base-url", server.URL}, statePath, store)
	if err == nil {
		t.Fatal("runConfigure() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "validate API key") {
		t.Fatalf("runConfigure() error = %v, want validation error", err)
	}
	if store.APIKey != "" {
		t.Fatalf("store.APIKey = %q, want empty after failed validation", store.APIKey)
	}
	if store.APIKeyValidatedAt != nil {
		t.Fatal("store.APIKeyValidatedAt != nil after failed validation")
	}
}

func TestRunConfigureRejectsUserAgentMetadataFlags(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := &config.Store{Version: config.CurrentVersion}
	app := &App{Version: "test"}

	err := app.runConfigure(context.Background(), []string{"--contact-email", "ops@example.com"}, statePath, store)
	if err == nil {
		t.Fatal("runConfigure() error = nil, want unknown-flag error")
	}
	if !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("runConfigure() error = %v, want unknown-flag error", err)
	}
}

func TestNewClientRequiresVerifiedConfiguredKey(t *testing.T) {
	app := &App{Version: "test"}
	store := &config.Store{
		Version:    config.CurrentVersion,
		APIKey:     "valid-key",
		APIBaseURL: "https://api.regfish.com",
	}

	if _, err := app.newClient(rootOptions{}, store); err == nil || !strings.Contains(err.Error(), "no verified API key configured") {
		t.Fatalf("newClient() error = %v, want verified-key error", err)
	}

	now := time.Now().UTC()
	store.APIKeyValidatedAt = &now
	if _, err := app.newClient(rootOptions{}, store); err != nil {
		t.Fatalf("newClient() error = %v, want nil", err)
	}

	if _, err := app.newClient(rootOptions{APIKey: "other-key"}, store); err == nil || !strings.Contains(err.Error(), "differs from the verified configured key") {
		t.Fatalf("newClient() override error = %v, want mismatch error", err)
	}
}

func TestNewClientIgnoresUserAgentInstanceEnvVar(t *testing.T) {
	t.Setenv("CERTBRO_USER_AGENT_INSTANCE", "env-override")

	now := time.Now().UTC()
	app := &App{Version: "test"}
	store := &config.Store{
		Version:           config.CurrentVersion,
		APIKey:            "valid-key",
		APIBaseURL:        "https://api.regfish.com",
		APIKeyValidatedAt: &now,
		UserAgentInstance: "configured-instance",
	}

	client, err := app.newClient(rootOptions{}, store)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	if !strings.Contains(client.UserAgent, "instance=configured-instance") {
		t.Fatalf("client.UserAgent = %q, want configured instance", client.UserAgent)
	}
	if strings.Contains(client.UserAgent, "env-override") {
		t.Fatalf("client.UserAgent = %q, must not use env override", client.UserAgent)
	}
}

func TestStoreForRenewPreservesVerifiedAPIConfiguration(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	validatedAt := time.Now().UTC().Round(time.Second)

	managed := config.ManagedCertificate{
		Name:          "example-com",
		CommonName:    "example.com",
		OutputDir:     outputDir,
		CertificateID: "CERT123",
	}
	if err := config.SaveManagedCertificate(outputDir, managed); err != nil {
		t.Fatalf("SaveManagedCertificate() error = %v", err)
	}

	store := &config.Store{
		Version:           config.CurrentVersion,
		APIKey:            "valid-key",
		APIBaseURL:        "https://api.regfish.com",
		APIKeyValidatedAt: &validatedAt,
		ContactEmail:      "ops@example.com",
		UserAgentInstance: "host-01",
	}

	app := &App{}
	renewStore, err := app.storeForRenew(rootOptions{CertificatesDir: root}, store)
	if err != nil {
		t.Fatalf("storeForRenew() error = %v", err)
	}
	if renewStore.APIKeyValidatedAt == nil || !renewStore.APIKeyValidatedAt.Equal(validatedAt) {
		t.Fatalf("renewStore.APIKeyValidatedAt = %v, want %v", renewStore.APIKeyValidatedAt, validatedAt)
	}
	if renewStore.APIKey != store.APIKey || renewStore.APIBaseURL != store.APIBaseURL {
		t.Fatalf("renewStore API configuration = %#v, want preserved global configuration", renewStore)
	}
	if len(renewStore.ManagedCertificates) != 1 || renewStore.ManagedCertificates[0].Name != "example-com" {
		t.Fatalf("renewStore.ManagedCertificates = %#v", renewStore.ManagedCertificates)
	}
}
