// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPathUsesLinuxSystemDefault(t *testing.T) {
	t.Setenv("CERTBRO_STATE_FILE", "")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if got != DefaultStateFilePath {
		t.Fatalf("DefaultPath() = %q, want %q", got, DefaultStateFilePath)
	}
}

func TestDefaultPathPrefersEnvironmentOverride(t *testing.T) {
	t.Setenv("CERTBRO_STATE_FILE", "/tmp/custom-state.json")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if got != "/tmp/custom-state.json" {
		t.Fatalf("DefaultPath() = %q, want env override", got)
	}
}

func TestSaveAndLoadStorePreservesUserAgentMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &Store{
		APIKey:            "secret",
		APIBaseURL:        "https://api.regfish.example",
		ContactEmail:      "ops@example.com",
		UserAgentInstance: "web-01",
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.APIKey != want.APIKey || got.APIBaseURL != want.APIBaseURL || got.ContactEmail != want.ContactEmail || got.UserAgentInstance != want.UserAgentInstance {
		t.Fatalf("loaded store = %#v, want %#v", *got, *want)
	}
}

func TestSaveLoadAndDiscoverManagedCertificates(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")

	want := ManagedCertificate{
		Name:       "example-com",
		CommonName: "example.com",
		Product:    "RapidSSL",
		OutputDir:  outputDir,
	}

	if err := SaveManagedCertificate(outputDir, want); err != nil {
		t.Fatalf("SaveManagedCertificate() error = %v", err)
	}

	got, err := LoadManagedCertificate(filepath.Join(outputDir, ManagedCertFileName))
	if err != nil {
		t.Fatalf("LoadManagedCertificate() error = %v", err)
	}
	if got.Name != want.Name || got.CommonName != want.CommonName || got.OutputDir != want.OutputDir {
		t.Fatalf("loaded certificate = %#v, want %#v", *got, want)
	}

	if err := os.MkdirAll(filepath.Join(outputDir, "live"), 0o755); err != nil {
		t.Fatalf("mkdir live: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "live", ManagedCertFileName), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write ignored nested file: %v", err)
	}

	discovered, err := DiscoverManagedCertificates(root)
	if err != nil {
		t.Fatalf("DiscoverManagedCertificates() error = %v", err)
	}
	if len(discovered) != 1 {
		t.Fatalf("len(discovered) = %d, want 1", len(discovered))
	}
	if discovered[0].Name != want.Name {
		t.Fatalf("discovered[0].Name = %q, want %q", discovered[0].Name, want.Name)
	}
}

func TestValidityScheduleFollowsCABForumTimeline(t *testing.T) {
	tests := []struct {
		name                string
		now                 time.Time
		wantMaxValidityDays int
		wantDefaultDays     int
	}{
		{
			name:                "before certbro safety switch to 200 day era",
			now:                 time.Date(2026, 3, 13, 23, 59, 59, 0, time.UTC),
			wantMaxValidityDays: 398,
			wantDefaultDays:     397,
		},
		{
			name:                "200 day era starts one day early in certbro",
			now:                 time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
			wantMaxValidityDays: 200,
			wantDefaultDays:     199,
		},
		{
			name:                "100 day era starts one day early in certbro",
			now:                 time.Date(2027, 3, 14, 0, 0, 0, 0, time.UTC),
			wantMaxValidityDays: 100,
			wantDefaultDays:     99,
		},
		{
			name:                "47 day era starts one day early in certbro",
			now:                 time.Date(2029, 3, 14, 0, 0, 0, 0, time.UTC),
			wantMaxValidityDays: 47,
			wantDefaultDays:     46,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaxValidityDaysAt(tc.now); got != tc.wantMaxValidityDays {
				t.Fatalf("MaxValidityDaysAt() = %d, want %d", got, tc.wantMaxValidityDays)
			}
			if got := DefaultValidityDaysAt(tc.now); got != tc.wantDefaultDays {
				t.Fatalf("DefaultValidityDaysAt() = %d, want %d", got, tc.wantDefaultDays)
			}
		})
	}
}

func TestApplyDefaultsUsesScheduleAwareValidity(t *testing.T) {
	var cert ManagedCertificate
	cert.applyDefaultsAt(time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC))

	if cert.ValidityDays != 99 {
		t.Fatalf("cert.ValidityDays = %d, want 99", cert.ValidityDays)
	}
}

func TestValidateValidityDaysAtRejectsValuesAboveCurrentLimit(t *testing.T) {
	err := ValidateValidityDaysAt(101, time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("ValidateValidityDaysAt() error = nil, want error")
	}
	if got := err.Error(); got == "" || got == "--validity-days must be greater than zero" {
		t.Fatalf("ValidateValidityDaysAt() error = %q, want limit error", got)
	}
}

func TestNormalizeStoredValidityDaysAtAdjustsLegacyValueToScheduleDefault(t *testing.T) {
	effective, adjusted, officialEffectiveFrom := NormalizeStoredValidityDaysAt(199, time.Date(2027, 3, 14, 0, 0, 0, 0, time.UTC))
	if !adjusted {
		t.Fatal("adjusted = false, want true")
	}
	if effective != 99 {
		t.Fatalf("effective = %d, want 99", effective)
	}
	if !officialEffectiveFrom.Equal(time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("officialEffectiveFrom = %v, want 2027-03-15", officialEffectiveFrom)
	}
}

func TestValidateRenewalTimingRejectsImmediateRenewalLoopConfiguration(t *testing.T) {
	err := ValidateRenewalTiming(3, 7, 2)
	if err == nil {
		t.Fatal("ValidateRenewalTiming() error = nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, "--renew-before-days") {
		t.Fatalf("ValidateRenewalTiming() error = %q, want renew-before-days error", got)
	}
}

func TestValidateRenewalTimingRejectsImmediateReissueLoopConfiguration(t *testing.T) {
	err := ValidateRenewalTiming(3, 2, 7)
	if err == nil {
		t.Fatal("ValidateRenewalTiming() error = nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, "--reissue-lead-days") {
		t.Fatalf("ValidateRenewalTiming() error = %q, want reissue-lead-days error", got)
	}
}

func TestNormalizeStoredRenewalTimingAdjustsLegacyLeadDays(t *testing.T) {
	renewBeforeDays, reissueLeadDays, adjusted, err := NormalizeStoredRenewalTiming(3, 7, 7)
	if err != nil {
		t.Fatalf("NormalizeStoredRenewalTiming() error = %v", err)
	}
	if !adjusted {
		t.Fatal("adjusted = false, want true")
	}
	if renewBeforeDays != 2 || reissueLeadDays != 2 {
		t.Fatalf("effective lead days = %d/%d, want 2/2", renewBeforeDays, reissueLeadDays)
	}
}
