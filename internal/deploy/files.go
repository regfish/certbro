// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package deploy writes certificate artifacts and runs post-deploy hooks.
package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	certcrypto "github.com/regfish/certbro/internal/crypto"
	"github.com/regfish/certbro/internal/tlsmeta"
)

// PendingMetadata describes an unfinished issuance or reissue attempt on disk.
type PendingMetadata struct {
	Action                 string                 `json:"action"`
	CertificateID          string                 `json:"certificate_id,omitempty"`
	CommonName             string                 `json:"common_name"`
	DNSNames               []string               `json:"dns_names,omitempty"`
	Product                string                 `json:"product"`
	ProductValidationLevel string                 `json:"product_validation_level,omitempty"`
	OrganizationRequired   bool                   `json:"organization_required,omitempty"`
	RequestedAt            time.Time              `json:"requested_at"`
	RequestedValidityDays  int                    `json:"requested_validity_days,omitempty"`
	OrganizationID         tlsmeta.OrganizationID `json:"organization_id,omitempty"`
	ActionRequired         bool                   `json:"action_required,omitempty"`
	PendingReason          string                 `json:"pending_reason,omitempty"`
	PendingMessage         string                 `json:"pending_message,omitempty"`
	CompletionURL          string                 `json:"completion_url,omitempty"`
}

// PendingMaterial is the persisted key material required to resume a pending request.
type PendingMaterial struct {
	PrivateKeyPEM []byte
	CSRPEM        []byte
	Metadata      PendingMetadata
}

// Artifact describes the deployment payload written to archive and live directories.
type Artifact struct {
	Name               string
	OutputDir          string
	CertificateID      string
	CommonName         string
	DNSNames           []string
	Product            string
	Status             string
	OrderState         string
	Action             string
	PrivateKeyPEM      []byte
	CSRPEM             []byte
	FullChainPEM       []byte
	BundleZIP          []byte
	ValidityDays       int
	ValidFrom          *time.Time
	ValidUntil         *time.Time
	ContractValidUntil *time.Time
}

// Result contains the final live and archive paths created during deployment.
type Result struct {
	ArchiveDir     string
	LiveDir        string
	FullChainPath  string
	CertPath       string
	ChainPath      string
	PrivateKeyPath string
	CSRPath        string
	BundleZIPPath  string
	MetadataPath   string
}

// WritePending stores the key material required to resume a pending order or reissue.
func WritePending(outputDir string, material PendingMaterial) error {
	pendingDir := filepath.Join(outputDir, "pending")
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		return fmt.Errorf("create pending dir %s: %w", pendingDir, err)
	}

	metaRaw, err := json.MarshalIndent(material.Metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pending metadata: %w", err)
	}
	metaRaw = append(metaRaw, '\n')

	if err := writeFileAtomic(filepath.Join(pendingDir, "privkey.pem"), material.PrivateKeyPEM, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pendingDir, "request.csr.pem"), material.CSRPEM, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pendingDir, "request.json"), metaRaw, 0o600); err != nil {
		return err
	}

	return nil
}

// LoadPendingMetadata reloads only the persisted pending request metadata.
func LoadPendingMetadata(outputDir string) (*PendingMetadata, error) {
	pendingDir := filepath.Join(outputDir, "pending")
	metaRaw, err := os.ReadFile(filepath.Join(pendingDir, "request.json"))
	if err != nil {
		return nil, fmt.Errorf("read pending request metadata: %w", err)
	}

	var metadata PendingMetadata
	if err := json.Unmarshal(metaRaw, &metadata); err != nil {
		return nil, fmt.Errorf("parse pending request metadata: %w", err)
	}
	return &metadata, nil
}

// LoadPending reloads previously persisted pending key material.
func LoadPending(outputDir string) (*PendingMaterial, error) {
	pendingDir := filepath.Join(outputDir, "pending")
	privateKeyPEM, err := os.ReadFile(filepath.Join(pendingDir, "privkey.pem"))
	if err != nil {
		return nil, fmt.Errorf("read pending private key: %w", err)
	}
	csrPEM, err := os.ReadFile(filepath.Join(pendingDir, "request.csr.pem"))
	if err != nil {
		return nil, fmt.Errorf("read pending CSR: %w", err)
	}
	metadata, err := LoadPendingMetadata(outputDir)
	if err != nil {
		return nil, err
	}

	return &PendingMaterial{
		PrivateKeyPEM: privateKeyPEM,
		CSRPEM:        csrPEM,
		Metadata:      *metadata,
	}, nil
}

// ClearPending removes the pending state directory after a successful deployment.
func ClearPending(outputDir string) error {
	pendingDir := filepath.Join(outputDir, "pending")
	if err := os.RemoveAll(pendingDir); err != nil {
		return fmt.Errorf("remove pending dir %s: %w", pendingDir, err)
	}
	return nil
}

// WriteArtifacts writes live and archived certificate artifacts atomically.
func WriteArtifacts(artifact Artifact) (*Result, error) {
	certPEM, chainPEM, err := certcrypto.SplitFullChainPEM(artifact.FullChainPEM)
	if err != nil {
		return nil, err
	}

	ts := time.Now().UTC().Format("20060102T150405Z")
	archiveDir := filepath.Join(artifact.OutputDir, "archive", ts)
	liveDir := filepath.Join(artifact.OutputDir, "live")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return nil, fmt.Errorf("create archive dir %s: %w", archiveDir, err)
	}
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return nil, fmt.Errorf("create live dir %s: %w", liveDir, err)
	}

	metadata := map[string]any{
		"name":                 artifact.Name,
		"certificate_id":       artifact.CertificateID,
		"common_name":          artifact.CommonName,
		"dns_names":            artifact.DNSNames,
		"product":              artifact.Product,
		"status":               artifact.Status,
		"order_state":          artifact.OrderState,
		"action":               artifact.Action,
		"deployed_at":          time.Now().UTC().Format(time.RFC3339),
		"valid_from":           formatTime(artifact.ValidFrom),
		"valid_until":          formatTime(artifact.ValidUntil),
		"contract_valid_until": formatTime(artifact.ContractValidUntil),
	}
	if artifact.ValidityDays > 0 {
		metadata["validity_days"] = artifact.ValidityDays
	}
	if effectiveValidityDays, ok := issuedValidityDays(artifact.ValidFrom, artifact.ValidUntil); ok {
		metadata["effective_validity_days"] = effectiveValidityDays
		if artifact.ValidityDays > 0 {
			renewalBonusDays := effectiveValidityDays - artifact.ValidityDays
			if renewalBonusDays > 0 {
				metadata["renewal_bonus_days"] = renewalBonusDays
			}
		}
	}
	metadataRaw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal deployment metadata: %w", err)
	}
	metadataRaw = append(metadataRaw, '\n')

	files := []struct {
		Name string
		Data []byte
		Mode os.FileMode
	}{
		{Name: "privkey.pem", Data: artifact.PrivateKeyPEM, Mode: 0o600},
		{Name: "cert.pem", Data: certPEM, Mode: 0o644},
		{Name: "chain.pem", Data: chainPEM, Mode: 0o644},
		{Name: "fullchain.pem", Data: artifact.FullChainPEM, Mode: 0o644},
		{Name: "metadata.json", Data: metadataRaw, Mode: 0o644},
	}
	if len(artifact.CSRPEM) > 0 {
		files = append(files, struct {
			Name string
			Data []byte
			Mode os.FileMode
		}{Name: "request.csr.pem", Data: artifact.CSRPEM, Mode: 0o600})
	}
	if len(artifact.BundleZIP) > 0 {
		files = append(files, struct {
			Name string
			Data []byte
			Mode os.FileMode
		}{Name: "bundle.zip", Data: artifact.BundleZIP, Mode: 0o644})
	}

	for _, file := range files {
		if err := writeFileAtomic(filepath.Join(archiveDir, file.Name), file.Data, file.Mode); err != nil {
			return nil, err
		}
		if err := writeFileAtomic(filepath.Join(liveDir, file.Name), file.Data, file.Mode); err != nil {
			return nil, err
		}
	}

	result := &Result{
		ArchiveDir:     archiveDir,
		LiveDir:        liveDir,
		FullChainPath:  filepath.Join(liveDir, "fullchain.pem"),
		CertPath:       filepath.Join(liveDir, "cert.pem"),
		ChainPath:      filepath.Join(liveDir, "chain.pem"),
		PrivateKeyPath: filepath.Join(liveDir, "privkey.pem"),
		MetadataPath:   filepath.Join(liveDir, "metadata.json"),
	}
	if len(artifact.CSRPEM) > 0 {
		result.CSRPath = filepath.Join(liveDir, "request.csr.pem")
	}
	if len(artifact.BundleZIP) > 0 {
		result.BundleZIPPath = filepath.Join(liveDir, "bundle.zip")
	}
	return result, nil
}

// RunInstallHook executes an optional shell hook after deployment has succeeded.
func RunInstallHook(command string, env map[string]string) error {
	if command == "" {
		return nil
	}

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run install hook %q: %w", command, err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func formatTime(ts *time.Time) string {
	if ts == nil {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func issuedValidityDays(validFrom, validUntil *time.Time) (int, bool) {
	if validFrom == nil || validUntil == nil {
		return 0, false
	}
	if validUntil.Before(*validFrom) {
		return 0, false
	}
	return int(validUntil.Sub(*validFrom).Hours()/24 + 0.5), true
}
