// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package config stores certbro runtime configuration and per-certificate state on disk.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CurrentVersion is the schema version of the persisted global state file.
	CurrentVersion = 1
	// DefaultStateFilePath is the default global state file path for Linux deployments.
	DefaultStateFilePath = "/etc/certbro/state.json"
	// DefaultCertificatesDir is the default root directory for managed certificate trees.
	DefaultCertificatesDir = "/etc/certbro"
	// DefaultRenewBeforeDays is the default lead time before expiry for renewals.
	DefaultRenewBeforeDays = 7
	// DefaultReissueLeadDays is the default lead time used when reissue is required.
	DefaultReissueLeadDays = 7
	// DefaultRSABits is the default RSA key size used for new private keys.
	DefaultRSABits = 2048
	// DefaultKeyType is the default key algorithm used for key generation.
	DefaultKeyType = "rsa"
	// DefaultECDSACurve is the default elliptic curve used for ECDSA keys.
	DefaultECDSACurve = "p256"
	// ManagedCertFileName is the per-certificate metadata file written to each output directory.
	ManagedCertFileName = "certbro.json"
)

var validitySchedule = []struct {
	officialOnOrAfter time.Time
	certbroOnOrAfter  time.Time
	maxValidityDays   int
}{
	{
		officialOnOrAfter: time.Date(2029, 3, 15, 0, 0, 0, 0, time.UTC),
		certbroOnOrAfter:  time.Date(2029, 3, 14, 0, 0, 0, 0, time.UTC),
		maxValidityDays:   47,
	},
	{
		officialOnOrAfter: time.Date(2027, 3, 15, 0, 0, 0, 0, time.UTC),
		certbroOnOrAfter:  time.Date(2027, 3, 14, 0, 0, 0, 0, time.UTC),
		maxValidityDays:   100,
	},
	{
		officialOnOrAfter: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		certbroOnOrAfter:  time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC),
		maxValidityDays:   200,
	},
	{
		officialOnOrAfter: time.Time{},
		certbroOnOrAfter:  time.Time{},
		maxValidityDays:   398,
	},
}

// MaxValidityDaysAt returns the active CA/B Forum maximum certificate validity for the given time.
func MaxValidityDaysAt(now time.Time) int {
	_, maxValidityDays := activeValiditySchedule(now)
	return maxValidityDays
}

// DefaultValidityDaysAt returns the recommended default certificate validity for the given time.
func DefaultValidityDaysAt(now time.Time) int {
	maxValidityDays := MaxValidityDaysAt(now)
	if maxValidityDays <= 1 {
		return maxValidityDays
	}
	return maxValidityDays - 1
}

// NormalizeStoredValidityDaysAt adjusts an already stored validity to the current schedule-aware default when it exceeds today's limit.
func NormalizeStoredValidityDaysAt(validityDays int, now time.Time) (effective int, adjusted bool, officialEffectiveFrom time.Time) {
	officialEffectiveFrom, maxValidityDays := activeValiditySchedule(now)
	if validityDays <= 0 {
		return DefaultValidityDaysAt(now), true, officialEffectiveFrom
	}
	if validityDays > maxValidityDays {
		return DefaultValidityDaysAt(now), true, officialEffectiveFrom
	}
	return validityDays, false, officialEffectiveFrom
}

// ValidateValidityDaysAt checks whether a requested validity is positive and within the active CA/B Forum limit.
func ValidateValidityDaysAt(validityDays int, now time.Time) error {
	if validityDays <= 0 {
		return fmt.Errorf("--validity-days must be greater than zero")
	}

	effectiveFrom, maxValidityDays := activeValiditySchedule(now)
	if validityDays > maxValidityDays {
		return fmt.Errorf("--validity-days must not exceed %d days under the current CA/B Forum limit effective %s", maxValidityDays, effectiveFrom.Format("2006-01-02"))
	}
	return nil
}

// ValidateRenewalTiming ensures the managed renewal windows cannot cause immediate follow-up renewals or reissues.
func ValidateRenewalTiming(validityDays, renewBeforeDays, reissueLeadDays int) error {
	if renewBeforeDays <= 0 {
		return fmt.Errorf("--renew-before-days must be greater than zero")
	}
	if reissueLeadDays <= 0 {
		return fmt.Errorf("--reissue-lead-days must be greater than zero")
	}
	if validityDays <= renewBeforeDays {
		return fmt.Errorf("--validity-days must be greater than --renew-before-days to avoid immediate renewal loops")
	}
	if validityDays <= reissueLeadDays {
		return fmt.Errorf("--validity-days must be greater than --reissue-lead-days to avoid immediate reissue loops")
	}
	return nil
}

// NormalizeStoredRenewalTiming reduces stored renewal windows that would otherwise trigger immediate follow-up renewals.
func NormalizeStoredRenewalTiming(validityDays, renewBeforeDays, reissueLeadDays int) (effectiveRenewBeforeDays, effectiveReissueLeadDays int, adjusted bool, err error) {
	if validityDays <= 1 {
		return 0, 0, false, fmt.Errorf("stored validity_days %d is too short for managed renewals; choose at least 2 days", validityDays)
	}

	effectiveRenewBeforeDays = renewBeforeDays
	if effectiveRenewBeforeDays <= 0 || effectiveRenewBeforeDays >= validityDays {
		effectiveRenewBeforeDays = validityDays - 1
		adjusted = true
	}

	effectiveReissueLeadDays = reissueLeadDays
	if effectiveReissueLeadDays <= 0 || effectiveReissueLeadDays >= validityDays {
		effectiveReissueLeadDays = validityDays - 1
		adjusted = true
	}

	return effectiveRenewBeforeDays, effectiveReissueLeadDays, adjusted, nil
}

func activeValiditySchedule(now time.Time) (time.Time, int) {
	now = now.UTC()
	for _, step := range validitySchedule {
		if step.certbroOnOrAfter.IsZero() || !now.Before(step.certbroOnOrAfter) {
			return step.officialOnOrAfter, step.maxValidityDays
		}
	}
	return time.Time{}, 398
}

// Store is the top-level persisted certbro configuration document.
type Store struct {
	Version             int                  `json:"version"`
	APIKey              string               `json:"api_key,omitempty"`
	APIBaseURL          string               `json:"api_base_url,omitempty"`
	APIKeyValidatedAt   *time.Time           `json:"api_key_validated_at,omitempty"`
	ContactEmail        string               `json:"contact_email,omitempty"`
	UserAgentInstance   string               `json:"user_agent_instance,omitempty"`
	ManagedCertificates []ManagedCertificate `json:"managed_certificates,omitempty"`
}

// ManagedCertificate is the persisted management state for one certificate directory.
type ManagedCertificate struct {
	Name               string     `json:"name"`
	CommonName         string     `json:"common_name"`
	DNSNames           []string   `json:"dns_names,omitempty"`
	Product            string     `json:"product"`
	OrganizationID     int        `json:"organization_id,omitempty"`
	ValidityDays       int        `json:"validity_days,omitempty"`
	OutputDir          string     `json:"output_dir"`
	InstallHook        string     `json:"install_hook,omitempty"`
	Webserver          string     `json:"webserver,omitempty"`
	WebserverConfig    string     `json:"webserver_config,omitempty"`
	KeyType            string     `json:"key_type,omitempty"`
	RSABits            int        `json:"rsa_bits,omitempty"`
	ECDSACurve         string     `json:"ecdsa_curve,omitempty"`
	RenewBeforeDays    int        `json:"renew_before_days,omitempty"`
	ReissueLeadDays    int        `json:"reissue_lead_days,omitempty"`
	CertificateID      string     `json:"certificate_id,omitempty"`
	Status             string     `json:"status,omitempty"`
	OrderState         string     `json:"order_state,omitempty"`
	PendingAction      string     `json:"pending_action,omitempty"`
	PendingStartedAt   *time.Time `json:"pending_started_at,omitempty"`
	LastIssuedAt       *time.Time `json:"last_issued_at,omitempty"`
	LastDeployedAt     *time.Time `json:"last_deployed_at,omitempty"`
	ValidFrom          *time.Time `json:"valid_from,omitempty"`
	ValidUntil         *time.Time `json:"valid_until,omitempty"`
	ContractValidUntil *time.Time `json:"contract_valid_until,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// DefaultPath returns the default certbro state file path.
func DefaultPath() (string, error) {
	if env := strings.TrimSpace(os.Getenv("CERTBRO_STATE_FILE")); env != "" {
		return env, nil
	}
	return DefaultStateFilePath, nil
}

// Load reads the global certbro state file, returning an empty store when it does not exist.
func Load(path string) (*Store, error) {
	store := &Store{Version: CurrentVersion}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, fmt.Errorf("read state file %s: %w", path, err)
	}

	if err := json.Unmarshal(raw, store); err != nil {
		return nil, fmt.Errorf("parse state file %s: %w", path, err)
	}

	if store.Version == 0 {
		store.Version = CurrentVersion
	}
	for i := range store.ManagedCertificates {
		store.ManagedCertificates[i].ApplyDefaults()
	}
	return store, nil
}

// Save writes the global certbro state file atomically.
func Save(path string, store *Store) error {
	store.Version = CurrentVersion
	for i := range store.ManagedCertificates {
		store.ManagedCertificates[i].ApplyDefaults()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir for %s: %w", path, err)
	}

	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state file %s: %w", path, err)
	}
	encoded = append(encoded, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp state file %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state file %s: %w", path, err)
	}
	return nil
}

// LoadManagedCertificate reads one per-certificate certbro.json file.
func LoadManagedCertificate(path string) (*ManagedCertificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read managed certificate file %s: %w", path, err)
	}

	var cert ManagedCertificate
	if err := json.Unmarshal(raw, &cert); err != nil {
		return nil, fmt.Errorf("parse managed certificate file %s: %w", path, err)
	}
	cert.ApplyDefaults()
	return &cert, nil
}

// SaveManagedCertificate writes one per-certificate certbro.json file atomically.
func SaveManagedCertificate(outputDir string, cert ManagedCertificate) error {
	cert.ApplyDefaults()
	path := filepath.Join(outputDir, ManagedCertFileName)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for managed certificate file %s: %w", path, err)
	}

	encoded, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal managed certificate file %s: %w", path, err)
	}
	encoded = append(encoded, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "certbro-*.json")
	if err != nil {
		return fmt.Errorf("create temp managed certificate file %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp managed certificate file %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp managed certificate file %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp managed certificate file %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace managed certificate file %s: %w", path, err)
	}
	return nil
}

// DiscoverManagedCertificates scans a root directory for certbro.json files.
func DiscoverManagedCertificates(rootDir string) ([]ManagedCertificate, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, nil
	}

	var discovered []ManagedCertificate
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			switch d.Name() {
			case "archive", "live", "pending":
				if path != rootDir {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if d.Name() != ManagedCertFileName {
			return nil
		}

		cert, err := LoadManagedCertificate(path)
		if err != nil {
			return err
		}
		discovered = append(discovered, *cert)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover managed certificates under %s: %w", rootDir, err)
	}
	return discovered, nil
}

// FindManagedCertificate locates a managed certificate by its logical name.
func (s *Store) FindManagedCertificate(name string) (*ManagedCertificate, int) {
	for i := range s.ManagedCertificates {
		if s.ManagedCertificates[i].Name == name {
			return &s.ManagedCertificates[i], i
		}
	}
	return nil, -1
}

// UpsertManagedCertificate inserts or replaces one managed certificate in the store.
func (s *Store) UpsertManagedCertificate(cert ManagedCertificate) {
	cert.ApplyDefaults()
	if existing, idx := s.FindManagedCertificate(cert.Name); existing != nil {
		s.ManagedCertificates[idx] = cert
		return
	}
	s.ManagedCertificates = append(s.ManagedCertificates, cert)
}

// ApplyDefaults normalizes legacy values and fills unset management defaults.
func (m *ManagedCertificate) ApplyDefaults() {
	m.applyDefaultsAt(time.Now().UTC())
}

func (m *ManagedCertificate) applyDefaultsAt(now time.Time) {
	if m.ValidityDays == 0 {
		m.ValidityDays = DefaultValidityDaysAt(now)
	}
	if strings.TrimSpace(m.KeyType) == "" {
		m.KeyType = DefaultKeyType
	}
	m.KeyType = strings.ToLower(strings.TrimSpace(m.KeyType))
	switch m.KeyType {
	case "ec", "ecc":
		m.KeyType = "ecdsa"
	}
	if m.RSABits == 0 {
		m.RSABits = DefaultRSABits
	}
	if strings.TrimSpace(m.ECDSACurve) == "" {
		m.ECDSACurve = DefaultECDSACurve
	}
	m.ECDSACurve = strings.ToLower(strings.TrimSpace(m.ECDSACurve))
	if m.RenewBeforeDays == 0 {
		m.RenewBeforeDays = DefaultRenewBeforeDays
	}
	if m.ReissueLeadDays == 0 {
		m.ReissueLeadDays = DefaultReissueLeadDays
	}
	m.Webserver = strings.ToLower(strings.TrimSpace(m.Webserver))
	switch m.Webserver {
	case "httpd", "apache2":
		m.Webserver = "apache"
	}
	m.WebserverConfig = strings.TrimSpace(m.WebserverConfig)
}
