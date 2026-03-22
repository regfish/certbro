// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/regfish/certbro/internal/api"
	"github.com/regfish/certbro/internal/config"
	certcrypto "github.com/regfish/certbro/internal/crypto"
	"github.com/regfish/certbro/internal/deploy"
)

// Manager coordinates certificate ordering, deployment, and renewal persistence.
type Manager struct {
	Client                      *api.Client
	Store                       *config.Store
	StorePath                   string
	Progress                    progressReporter
	provisionedValidationRecord map[string]struct{}
}

// OperationResult summarizes the effect of a single issue, import, or renewal action.
type OperationResult struct {
	Name          string
	Action        string
	Changed       bool
	CertificateID string
	Status        string
	LiveDir       string
	Message       string
}

type renewalAction string

const (
	renewalSkip            renewalAction = "skip"
	renewalCompletePending renewalAction = "complete-pending"
	renewalReissue         renewalAction = "reissue"
	renewalOrder           renewalAction = "renewal-order"
	renewalNewOrder        renewalAction = "new-order"
)

const certbroManagedDNSAnnotationPrefix = "managed by certbro"

// NewManager constructs a Manager bound to one API client and one local store.
func NewManager(client *api.Client, store *config.Store, storePath string) *Manager {
	return &Manager{
		Client:                      client,
		Store:                       store,
		StorePath:                   storePath,
		Progress:                    nopProgressReporter{},
		provisionedValidationRecord: make(map[string]struct{}),
	}
}

type waitTimeoutError struct {
	CertificateID string
	Status        string
}

func (e *waitTimeoutError) Error() string {
	return fmt.Sprintf("timeout waiting for certificate %s, last status %s", e.CertificateID, emptyFallback(e.Status, "unknown"))
}

// Issue creates a fresh certificate order and deploys the result once issued.
func (m *Manager) Issue(ctx context.Context, managed config.ManagedCertificate, waitTimeout, waitInterval time.Duration) (*OperationResult, error) {
	managed.ApplyDefaults()
	now := time.Now().UTC()
	if managed.CreatedAt.IsZero() {
		managed.CreatedAt = now
	}
	managed.UpdatedAt = now

	absOutputDir, err := filepath.Abs(managed.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve output dir %s: %w", managed.OutputDir, err)
	}
	managed.OutputDir = absOutputDir

	return m.startOrder(ctx, &managed, waitTimeout, waitInterval, "issue", "")
}

// Import registers an existing issued certificate for future management and optional deployment.
func (m *Manager) Import(ctx context.Context, managed config.ManagedCertificate, privateKeyPEM, csrPEM []byte) (*OperationResult, error) {
	managed.ApplyDefaults()
	if strings.TrimSpace(managed.CertificateID) == "" {
		return nil, errors.New("missing certificate_id")
	}

	absOutputDir, err := filepath.Abs(managed.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve output dir %s: %w", managed.OutputDir, err)
	}
	managed.OutputDir = absOutputDir

	remote, err := m.Client.GetCertificate(ctx, managed.CertificateID)
	if err != nil {
		return nil, err
	}
	if remote.Status == "pending" || (remote.Reissue != nil && isActiveReissue(remote.Reissue.Status)) {
		return nil, errors.New("importing pending certificates is not supported yet; wait until the certificate has been issued")
	}

	managed.CertificateID = remote.ID
	managed.CommonName = remote.CommonName
	managed.DNSNames = certcrypto.NormalizeDNSNames("", remote.DNSNames)
	managed.Product = remote.Product
	if remote.ValidityDays > 0 {
		managed.ValidityDays = remote.ValidityDays
	}
	if strings.TrimSpace(managed.Name) == "" {
		managed.Name = strings.TrimSpace(strings.ToLower(remote.CommonName))
	}

	if existing, idx := m.Store.FindManagedCertificate(managed.Name); existing != nil && m.Store.ManagedCertificates[idx].CertificateID != managed.CertificateID {
		return nil, fmt.Errorf("managed certificate %q already exists", managed.Name)
	}

	now := time.Now().UTC()
	if managed.CreatedAt.IsZero() {
		managed.CreatedAt = now
	}
	m.applyRemoteState(&managed, remote)
	if err := m.persistManaged(managed); err != nil {
		return nil, err
	}

	result := &OperationResult{
		Name:          managed.Name,
		Action:        "import",
		Changed:       true,
		CertificateID: managed.CertificateID,
		Status:        managed.Status,
		Message:       "certificate imported",
	}

	if len(privateKeyPEM) == 0 {
		result.Message = "certificate imported for renewal management"
		return result, nil
	}
	if !remote.CertificateAvailable {
		return nil, errors.New("the certificate is not downloadable yet")
	}

	fullChainPEM, err := m.Client.DownloadCertificate(ctx, remote.ID, "pem")
	if err != nil {
		return nil, err
	}
	deployResult, err := deploy.WriteArtifacts(deploy.Artifact{
		Name:               managed.Name,
		OutputDir:          managed.OutputDir,
		CertificateID:      remote.ID,
		CommonName:         remote.CommonName,
		DNSNames:           remote.DNSNames,
		Product:            remote.Product,
		Status:             remote.Status,
		OrderState:         remote.OrderState,
		Action:             "import",
		PrivateKeyPEM:      privateKeyPEM,
		CSRPEM:             csrPEM,
		FullChainPEM:       fullChainPEM,
		ValidFrom:          remote.ValidFrom,
		ValidUntil:         remote.ValidUntil,
		ContractValidUntil: remote.ContractValidUntil,
	})
	if err != nil {
		return nil, err
	}
	if err := m.postDeploy(&managed, remote, deployResult); err != nil {
		return nil, err
	}

	deployedAt := time.Now().UTC()
	managed.LastDeployedAt = &deployedAt
	if err := m.persistManaged(managed); err != nil {
		return nil, err
	}

	result.LiveDir = deployResult.LiveDir
	result.Message = "certificate imported and deployed"
	return result, nil
}

// Renew evaluates all selected managed certificates and executes the required renewal action.
func (m *Manager) Renew(ctx context.Context, names []string, force bool, validityDaysOverride int, waitTimeout, waitInterval time.Duration) ([]OperationResult, error) {
	var targets []int
	if len(names) == 0 {
		for i := range m.Store.ManagedCertificates {
			targets = append(targets, i)
		}
	} else {
		for _, name := range names {
			_, idx := m.Store.FindManagedCertificate(name)
			if idx < 0 {
				return nil, fmt.Errorf("managed certificate %q not found", name)
			}
			targets = append(targets, idx)
		}
	}

	results := make([]OperationResult, 0, len(targets))
	for _, idx := range targets {
		managed := m.Store.ManagedCertificates[idx]
		result, err := m.renewOne(ctx, &managed, force, validityDaysOverride, waitTimeout, waitInterval)
		if err != nil {
			return results, fmt.Errorf("renew %s: %w", managed.Name, err)
		}
		results = append(results, *result)
	}
	return results, nil
}

// renewOne resolves the correct renewal strategy for one managed certificate.
func (m *Manager) renewOne(ctx context.Context, managed *config.ManagedCertificate, force bool, validityDaysOverride int, waitTimeout, waitInterval time.Duration) (*OperationResult, error) {
	managed.ApplyDefaults()
	now := time.Now().UTC()

	var remote *api.TLSCertificate
	var err error
	if managed.CertificateID != "" {
		remote, err = m.Client.GetCertificate(ctx, managed.CertificateID)
		if err != nil {
			if api.IsStatus(err, http.StatusNotFound) {
				remote = nil
			} else {
				return nil, err
			}
		}
	}

	action := planRenewal(now, *managed, remote, force)
	if validityDaysOverride > 0 {
		switch action {
		case renewalOrder, renewalNewOrder:
			managed.ValidityDays = validityDaysOverride
		case renewalReissue:
			return nil, fmt.Errorf("--validity-days cannot be applied to a reissue; reissues inherit the existing contract")
		case renewalCompletePending:
			return nil, fmt.Errorf("--validity-days cannot be applied while a pending request is being resumed")
		}
	}
	switch action {
	case renewalSkip:
		if remote != nil {
			m.applyRemoteState(managed, remote)
			if err := m.persistManaged(*managed); err != nil {
				return nil, err
			}
		}
		return &OperationResult{
			Name:          managed.Name,
			Action:        string(action),
			Changed:       false,
			CertificateID: managed.CertificateID,
			Status:        managed.Status,
			Message:       "renewal not due",
		}, nil

	case renewalCompletePending:
		if remote == nil {
			return nil, errors.New("pending order exists in state, but the remote certificate could not be loaded")
		}
		pendingMaterial, err := deploy.LoadPending(managed.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("load pending key material from %s: %w", managed.OutputDir, err)
		}
		pendingAction := managed.PendingAction
		if pendingAction == "" {
			if remote.Reissue != nil && isActiveReissue(remote.Reissue.Status) {
				pendingAction = "reissue"
			} else {
				pendingAction = "issue"
			}
		}
		m.progress().WaitStart("%s: resuming pending %s for certificate_id %s, still waiting for issuance and this can take a few minutes", managed.Name, pendingAction, remote.ID)
		finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, remote.ID, pendingAction, managed.ValidUntil, waitTimeout, waitInterval)
		if err != nil {
			return nil, withPendingResumeHint(err, managed.Name)
		}
		return m.finishDeployment(managed, pendingMaterial.PrivateKeyPEM, pendingMaterial.CSRPEM, finalCert, fullChainPEM, bundleZIP, pendingAction, "completed pending request")

	case renewalReissue:
		if remote == nil {
			return nil, errors.New("cannot reissue without a known remote certificate")
		}
		return m.performReissue(ctx, managed, remote, waitTimeout, waitInterval)

	case renewalOrder:
		if remote == nil {
			return nil, errors.New("cannot renew without a known remote certificate")
		}
		return m.startOrder(ctx, managed, waitTimeout, waitInterval, string(action), remote.ID)

	case renewalNewOrder:
		return m.startOrder(ctx, managed, waitTimeout, waitInterval, string(action), "")

	default:
		return nil, fmt.Errorf("unsupported renewal action %q", action)
	}
}

// startOrder creates a new certificate or renewal order and deploys it after issuance.
func (m *Manager) startOrder(ctx context.Context, managed *config.ManagedCertificate, waitTimeout, waitInterval time.Duration, action, renewalOfCertificateID string) (*OperationResult, error) {
	managed.ApplyDefaults()
	if err := config.ValidateValidityDaysAt(managed.ValidityDays, time.Now().UTC()); err != nil {
		return nil, err
	}

	m.progress().Stepf("%s: validating TLS product %s", managed.Name, managed.Product)
	productSKU, err := m.resolveProductSKU(ctx, managed.Product)
	if err != nil {
		return nil, err
	}
	managed.Product = productSKU

	m.progress().Stepf("%s: generating %s key and CSR", managed.Name, strings.ToUpper(managed.KeyType))
	material, err := certcrypto.GenerateKeyAndCSR(managed.CommonName, managed.DNSNames, certcrypto.KeyOptions{
		Type:       managed.KeyType,
		RSABits:    managed.RSABits,
		ECDSACurve: managed.ECDSACurve,
	})
	if err != nil {
		return nil, err
	}

	pendingMeta := deploy.PendingMetadata{
		Action:      action,
		CommonName:  managed.CommonName,
		DNSNames:    managed.DNSNames,
		Product:     managed.Product,
		RequestedAt: time.Now().UTC(),
	}
	if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
		PrivateKeyPEM: material.PrivateKeyPEM,
		CSRPEM:        material.CSRPEM,
		Metadata:      pendingMeta,
	}); err != nil {
		return nil, err
	}

	switch {
	case renewalOfCertificateID != "":
		m.progress().Stepf("%s: starting renewal order for certificate_id %s", managed.Name, renewalOfCertificateID)
	default:
		m.progress().Stepf("%s: starting certificate order", managed.Name)
	}
	cert, err := m.Client.CreateCertificate(ctx, api.TLSCertificateRequest{
		SKU:                    managed.Product,
		CommonName:             managed.CommonName,
		DNSNames:               managed.DNSNames,
		CSR:                    string(material.CSRPEM),
		DCVMethod:              "dns-cname-token",
		Organization:           managed.OrganizationID,
		RenewalOfCertificateID: renewalOfCertificateID,
		ValidityDays:           managed.ValidityDays,
	})
	if err != nil {
		return nil, err
	}

	managed.CertificateID = cert.ID
	managed.PendingAction = action
	now := time.Now().UTC()
	managed.PendingStartedAt = &now
	m.applyRemoteState(managed, cert)
	if err := m.persistManaged(*managed); err != nil {
		return nil, err
	}

	pendingMeta.CertificateID = cert.ID
	if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
		PrivateKeyPEM: material.PrivateKeyPEM,
		CSRPEM:        material.CSRPEM,
		Metadata:      pendingMeta,
	}); err != nil {
		return nil, err
	}

	if err := m.ensureValidationRecords(ctx, cert); err != nil {
		return nil, err
	}

	m.progress().WaitStart("%s: waiting for issuance of certificate_id %s, this can take a few minutes", managed.Name, cert.ID)
	finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, cert.ID, action, nil, waitTimeout, waitInterval)
	if err != nil {
		return nil, withPendingResumeHint(err, managed.Name)
	}

	message := "certificate issued"
	if renewalOfCertificateID != "" {
		message = "certificate renewed"
	}
	return m.finishDeployment(managed, material.PrivateKeyPEM, material.CSRPEM, finalCert, fullChainPEM, bundleZIP, action, message)
}

// resolveProductSKU looks up the canonical product SKU from the live TLS product catalog.
func (m *Manager) resolveProductSKU(ctx context.Context, wanted string) (string, error) {
	products, err := m.Client.ListTLSProducts(ctx)
	if err != nil {
		return "", fmt.Errorf("list TLS products: %w", err)
	}

	return m.resolveProductSKUFromCatalog(products, wanted)
}

// resolveProductSKUFromCatalog validates a product against a previously fetched product catalog.
func (m *Manager) resolveProductSKUFromCatalog(products []api.TLSProduct, wanted string) (string, error) {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return "", errors.New("missing TLS product")
	}

	for _, product := range products {
		if strings.EqualFold(product.SKU, wanted) {
			return product.SKU, nil
		}
	}

	available := make([]string, 0, len(products))
	for _, product := range products {
		if strings.TrimSpace(product.SKU) == "" {
			continue
		}
		available = append(available, product.SKU)
	}
	sort.Strings(available)
	if len(available) == 0 {
		return "", fmt.Errorf("unknown TLS product %q and the product catalog is empty", wanted)
	}
	return "", fmt.Errorf("unknown TLS product %q; available products: %s", wanted, strings.Join(available, ", "))
}

// performReissue submits a reissue request for the existing contract and deploys the result.
func (m *Manager) performReissue(ctx context.Context, managed *config.ManagedCertificate, remote *api.TLSCertificate, waitTimeout, waitInterval time.Duration) (*OperationResult, error) {
	m.progress().Stepf("%s: generating %s key and CSR for reissue", managed.Name, strings.ToUpper(managed.KeyType))
	material, err := certcrypto.GenerateKeyAndCSR(managed.CommonName, managed.DNSNames, certcrypto.KeyOptions{
		Type:       managed.KeyType,
		RSABits:    managed.RSABits,
		ECDSACurve: managed.ECDSACurve,
	})
	if err != nil {
		return nil, err
	}

	pendingMeta := deploy.PendingMetadata{
		Action:        "reissue",
		CertificateID: remote.ID,
		CommonName:    managed.CommonName,
		DNSNames:      managed.DNSNames,
		Product:       managed.Product,
		RequestedAt:   time.Now().UTC(),
	}
	if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
		PrivateKeyPEM: material.PrivateKeyPEM,
		CSRPEM:        material.CSRPEM,
		Metadata:      pendingMeta,
	}); err != nil {
		return nil, err
	}

	m.progress().Stepf("%s: starting reissue for certificate_id %s", managed.Name, remote.ID)
	reissued, err := m.Client.ReissueCertificate(ctx, remote.ID, api.TLSCertificateReissueRequest{
		CSR:        string(material.CSRPEM),
		CommonName: managed.CommonName,
		DNSNames:   managed.DNSNames,
		DCVMethod:  "dns-cname-token",
		Comments:   "certbro scheduled reissue",
	})
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	managed.PendingAction = "reissue"
	managed.PendingStartedAt = &now
	m.applyRemoteState(managed, reissued)
	if err := m.persistManaged(*managed); err != nil {
		return nil, err
	}

	if err := m.ensureValidationRecords(ctx, reissued); err != nil {
		return nil, err
	}

	m.progress().WaitStart("%s: waiting for reissue of certificate_id %s, this can take a few minutes", managed.Name, remote.ID)
	finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, remote.ID, "reissue", remote.ValidUntil, waitTimeout, waitInterval)
	if err != nil {
		return nil, withPendingResumeHint(err, managed.Name)
	}

	return m.finishDeployment(managed, material.PrivateKeyPEM, material.CSRPEM, finalCert, fullChainPEM, bundleZIP, "reissue", "certificate reissued")
}

func (m *Manager) finishDeployment(managed *config.ManagedCertificate, privateKeyPEM, csrPEM []byte, cert *api.TLSCertificate, fullChainPEM, bundleZIP []byte, action, message string) (*OperationResult, error) {
	m.progress().Stepf("%s: downloading certificate and writing deployment artifacts", managed.Name)
	deployResult, err := deploy.WriteArtifacts(deploy.Artifact{
		Name:               managed.Name,
		OutputDir:          managed.OutputDir,
		CertificateID:      cert.ID,
		CommonName:         cert.CommonName,
		DNSNames:           cert.DNSNames,
		Product:            cert.Product,
		Status:             cert.Status,
		OrderState:         cert.OrderState,
		Action:             action,
		PrivateKeyPEM:      privateKeyPEM,
		CSRPEM:             csrPEM,
		FullChainPEM:       fullChainPEM,
		BundleZIP:          bundleZIP,
		ValidFrom:          cert.ValidFrom,
		ValidUntil:         cert.ValidUntil,
		ContractValidUntil: cert.ContractValidUntil,
	})
	if err != nil {
		return nil, err
	}

	m.progress().Stepf("%s: reloading configured webserver and running install hook", managed.Name)
	if err := m.postDeploy(managed, cert, deployResult); err != nil {
		return nil, err
	}

	if err := deploy.ClearPending(managed.OutputDir); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	managed.PendingAction = ""
	managed.PendingStartedAt = nil
	managed.LastIssuedAt = &now
	managed.LastDeployedAt = &now
	m.applyRemoteState(managed, cert)
	if err := m.persistManaged(*managed); err != nil {
		return nil, err
	}
	m.progress().WaitDone("%s: deployment finished", managed.Name)

	return &OperationResult{
		Name:          managed.Name,
		Action:        action,
		Changed:       true,
		CertificateID: cert.ID,
		Status:        cert.Status,
		LiveDir:       deployResult.LiveDir,
		Message:       message,
	}, nil
}

func (m *Manager) postDeploy(managed *config.ManagedCertificate, cert *api.TLSCertificate, deployResult *deploy.Result) error {
	hookEnv := map[string]string{
		"CERTBRO_NAME":             managed.Name,
		"CERTBRO_CERTIFICATE_ID":   cert.ID,
		"CERTBRO_OUTPUT_DIR":       managed.OutputDir,
		"CERTBRO_LIVE_DIR":         deployResult.LiveDir,
		"CERTBRO_FULLCHAIN_PATH":   deployResult.FullChainPath,
		"CERTBRO_CERT_PATH":        deployResult.CertPath,
		"CERTBRO_CHAIN_PATH":       deployResult.ChainPath,
		"CERTBRO_PRIVKEY_PATH":     deployResult.PrivateKeyPath,
		"CERTBRO_CSR_PATH":         deployResult.CSRPath,
		"CERTBRO_METADATA_PATH":    deployResult.MetadataPath,
		"CERTBRO_WEBSERVER":        managed.Webserver,
		"CERTBRO_WEBSERVER_CONFIG": managed.WebserverConfig,
	}
	if err := deploy.ReloadWebserver(deploy.WebserverIntegration{
		Kind:       managed.Webserver,
		ConfigPath: managed.WebserverConfig,
	}); err != nil {
		return err
	}
	if err := deploy.RunInstallHook(managed.InstallHook, hookEnv); err != nil {
		return err
	}
	return nil
}

func (m *Manager) ensureValidationRecords(ctx context.Context, cert *api.TLSCertificate) error {
	if m.provisionedValidationRecord == nil {
		m.provisionedValidationRecord = make(map[string]struct{})
	}

	seen := map[string]struct{}{}
	records := make([]api.TLSValidationDNSRecord, 0)

	if cert.Validation != nil && cert.Validation.Method == "dns-cname-token" {
		records = append(records, cert.Validation.DNSRecords...)
	}
	if cert.Reissue != nil && cert.Reissue.Validation != nil && cert.Reissue.Validation.Method == "dns-cname-token" {
		records = append(records, cert.Reissue.Validation.DNSRecords...)
	}

	for _, record := range records {
		key := record.Name + "|" + record.Value
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := m.provisionedValidationRecord[key]; ok {
			continue
		}

		if err := m.provisionValidationRecord(ctx, cert.ID, record); err != nil {
			return err
		}
		m.provisionedValidationRecord[key] = struct{}{}
	}

	return nil
}

func (m *Manager) provisionValidationRecord(ctx context.Context, certificateID string, record api.TLSValidationDNSRecord) error {
	recordName := strings.TrimSpace(record.Name)
	recordValue := strings.TrimSpace(record.Value)
	recordKey := recordName + "|" + recordValue
	m.progress().Stepf("provisioning DCV CNAME %s -> %s", recordName, recordValue)

	existing, zone, err := m.lookupValidationCNAMERecords(ctx, recordName)
	if err != nil {
		m.progress().Stepf("could not inspect existing DCV CNAMEs for %s, continuing with direct create", recordName)
		_, err := m.Client.CreateDNSRecord(ctx, api.DNSRecord{
			Type:       "CNAME",
			Name:       recordName,
			Data:       recordValue,
			TTL:        60,
			Annotation: validationRecordAnnotation(certificateID),
		})
		if err != nil {
			return fmt.Errorf("create validation CNAME %s -> %s: %w", recordName, recordValue, err)
		}
		m.provisionedValidationRecord[recordKey] = struct{}{}
		return nil
	}

	var certbroRecords []api.DNSRecord
	var foreignRecords []api.DNSRecord
	for _, existingRecord := range existing {
		if isCertbroManagedDNSRecord(existingRecord) {
			certbroRecords = append(certbroRecords, existingRecord)
		} else {
			foreignRecords = append(foreignRecords, existingRecord)
		}
	}

	if len(foreignRecords) > 0 {
		if len(existing) == 1 && sameDNSRecordData(foreignRecords[0].Data, recordValue) {
			m.progress().Stepf("DCV CNAME %s is already present and points to the expected value", recordName)
			return nil
		}
		return fmt.Errorf("refusing to modify existing non-certbro CNAME record(s) for %s in zone %s; remove the conflicting record(s) manually first", recordName, zone)
	}

	if len(certbroRecords) == 1 && sameDNSRecordData(certbroRecords[0].Data, recordValue) {
		m.progress().Stepf("DCV CNAME %s is already managed by certbro and points to the expected value", recordName)
		return nil
	}

	for _, existingRecord := range certbroRecords {
		if existingRecord.ID == 0 {
			return fmt.Errorf("cannot delete stale certbro-managed CNAME %s without rrid", existingRecord.Name)
		}
		m.progress().Stepf("removing stale certbro DCV CNAME %s -> %s", strings.TrimSpace(existingRecord.Name), strings.TrimSpace(existingRecord.Data))
		if err := m.Client.DeleteDNSRecord(ctx, existingRecord.ID); err != nil {
			return fmt.Errorf("delete stale validation CNAME rrid %d for %s: %w", existingRecord.ID, recordName, err)
		}
	}

	_, err = m.Client.CreateDNSRecord(ctx, api.DNSRecord{
		Type:       "CNAME",
		Name:       recordName,
		Data:       recordValue,
		TTL:        60,
		Annotation: validationRecordAnnotation(certificateID),
	})
	if err != nil {
		return fmt.Errorf("create validation CNAME %s -> %s: %w", recordName, recordValue, err)
	}
	return nil
}

func (m *Manager) waitAndDownload(ctx context.Context, certificateID, action string, previousValidUntil *time.Time, waitTimeout, waitInterval time.Duration) (*api.TLSCertificate, []byte, []byte, error) {
	if waitTimeout <= 0 {
		waitTimeout = 30 * time.Minute
	}
	if waitInterval <= 0 {
		waitInterval = 30 * time.Second
	}

	deadline := time.Now().Add(waitTimeout)
	for {
		cert, err := m.Client.GetCertificate(ctx, certificateID)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := m.ensureValidationRecords(ctx, cert); err != nil {
			return nil, nil, nil, err
		}

		if readyForDownload(cert, action, previousValidUntil) {
			m.progress().WaitDone("certificate_id %s is ready, downloading certificate", certificateID)
			fullChainPEM, err := m.Client.DownloadCertificate(ctx, certificateID, "pem")
			if err != nil {
				if api.IsStatus(err, http.StatusConflict) && time.Now().Before(deadline) {
					m.progress().WaitTick("certificate_id %s is issued, waiting for the download bundle, this can still take a moment", certificateID)
					if err := sleepContext(ctx, waitInterval); err != nil {
						return nil, nil, nil, err
					}
					continue
				}
				return nil, nil, nil, err
			}

			var bundleZIP []byte
			bundleZIP, err = m.Client.DownloadCertificate(ctx, certificateID, "zip")
			if err != nil && !api.IsStatus(err, http.StatusConflict) && !api.IsStatus(err, http.StatusNotFound) {
				return nil, nil, nil, err
			}
			return cert, fullChainPEM, bundleZIP, nil
		}

		if failedCertificateStatus(cert.Status) {
			m.progress().WaitDone("certificate_id %s failed with status %s", certificateID, cert.Status)
			return nil, nil, nil, fmt.Errorf("certificate %s ended in status %s", certificateID, cert.Status)
		}
		if cert.Reissue != nil && failedReissueStatus(cert.Reissue.Status) {
			m.progress().WaitDone("reissue for certificate_id %s failed with status %s", certificateID, cert.Reissue.Status)
			return nil, nil, nil, fmt.Errorf("reissue for certificate %s ended in status %s", certificateID, cert.Reissue.Status)
		}
		if time.Now().After(deadline) {
			m.progress().WaitDone("certificate_id %s timed out while waiting for issuance; rerun certbro renew to resume monitoring", certificateID)
			return nil, nil, nil, &waitTimeoutError{CertificateID: certificateID, Status: cert.Status}
		}
		m.progress().WaitTick("certificate_id %s status=%s order_state=%s, still processing and this can take a few minutes", certificateID, emptyFallback(cert.Status, "unknown"), emptyFallback(cert.OrderState, "unknown"))
		if err := sleepContext(ctx, waitInterval); err != nil {
			return nil, nil, nil, err
		}
	}
}

func withPendingResumeHint(err error, managedName string) error {
	var timeoutErr *waitTimeoutError
	if !errors.As(err, &timeoutErr) {
		return err
	}
	return fmt.Errorf("%w; rerun `certbro renew --name %s` to resume monitoring this pending request", err, managedName)
}

func (m *Manager) lookupValidationCNAMERecords(ctx context.Context, fqdn string) ([]api.DNSRecord, string, error) {
	candidates := zoneCandidates(fqdn)
	for _, candidate := range candidates {
		records, err := m.Client.ListDNSRecords(ctx, candidate)
		if err == nil {
			var matches []api.DNSRecord
			for _, record := range records {
				if !strings.EqualFold(strings.TrimSpace(record.Type), "CNAME") {
					continue
				}
				if !sameDNSRecordName(record.Name, fqdn) {
					continue
				}
				matches = append(matches, record)
			}
			return matches, candidate, nil
		}
		if api.IsStatus(err, http.StatusNotFound) {
			continue
		}
		return nil, "", err
	}
	return nil, "", fmt.Errorf("could not determine a managed regfish DNS zone for %s", fqdn)
}

func zoneCandidates(fqdn string) []string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(fqdn), ".")
	if trimmed == "" {
		return nil
	}
	labels := strings.Split(trimmed, ".")
	if len(labels) < 2 {
		return nil
	}

	candidates := make([]string, 0, len(labels)-1)
	for i := 1; i < len(labels)-1; i++ {
		candidates = append(candidates, strings.Join(labels[i:], "."))
	}
	if len(labels) == 2 {
		candidates = append(candidates, trimmed)
	}
	return candidates
}

func validationRecordAnnotation(certificateID string) string {
	certificateID = strings.TrimSpace(certificateID)
	if certificateID == "" {
		return certbroManagedDNSAnnotationPrefix
	}
	return certbroManagedDNSAnnotationPrefix + "; certificate_id=" + certificateID
}

func isCertbroManagedDNSRecord(record api.DNSRecord) bool {
	return strings.HasPrefix(strings.TrimSpace(record.Annotation), certbroManagedDNSAnnotationPrefix)
}

func sameDNSRecordName(left, right string) bool {
	return normalizeDNSFQDN(left) == normalizeDNSFQDN(right)
}

func sameDNSRecordData(left, right string) bool {
	return normalizeDNSFQDN(left) == normalizeDNSFQDN(right)
}

func normalizeDNSFQDN(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".")
	return strings.ToLower(value)
}

func (m *Manager) progress() progressReporter {
	if m == nil || m.Progress == nil {
		return nopProgressReporter{}
	}
	return m.Progress
}

func (m *Manager) applyRemoteState(managed *config.ManagedCertificate, cert *api.TLSCertificate) {
	managed.CertificateID = cert.ID
	managed.Status = cert.Status
	managed.OrderState = cert.OrderState
	managed.ValidFrom = cert.ValidFrom
	managed.ValidUntil = cert.ValidUntil
	managed.ContractValidUntil = cert.ContractValidUntil
	managed.UpdatedAt = time.Now().UTC()
}

func (m *Manager) persistManaged(managed config.ManagedCertificate) error {
	managed.UpdatedAt = time.Now().UTC()
	if err := config.SaveManagedCertificate(managed.OutputDir, managed); err != nil {
		return err
	}
	m.Store.UpsertManagedCertificate(managed)
	return config.Save(m.StorePath, m.Store)
}

func planRenewal(now time.Time, managed config.ManagedCertificate, remote *api.TLSCertificate, force bool) renewalAction {
	if managed.PendingAction != "" {
		return renewalCompletePending
	}
	if remote == nil {
		return renewalNewOrder
	}
	if remote.Status == "pending" {
		return renewalCompletePending
	}
	if remote.Reissue != nil && isActiveReissue(remote.Reissue.Status) {
		return renewalCompletePending
	}

	validUntil := pickTime(remote.ValidUntil, managed.ValidUntil)
	contractValidUntil := pickTime(remote.ContractValidUntil, managed.ContractValidUntil)
	preferReissue := shouldPreferReissue(remote, validUntil, contractValidUntil)
	leadDays := managed.RenewBeforeDays
	if preferReissue {
		leadDays = managed.ReissueLeadDays
	}

	if !force && !dueForRenewal(now, validUntil, leadDays) {
		return renewalSkip
	}

	if preferReissue && remote.ReissueSupported {
		return renewalReissue
	}

	if !remote.RenewalSupported {
		return renewalNewOrder
	}

	if remote.ID != "" {
		return renewalOrder
	}

	return renewalNewOrder
}

func dueForRenewal(now time.Time, validUntil *time.Time, leadDays int) bool {
	if validUntil == nil {
		return true
	}
	return !validUntil.After(now.Add(time.Duration(leadDays) * 24 * time.Hour))
}

func shouldPreferReissue(remote *api.TLSCertificate, validUntil, contractValidUntil *time.Time) bool {
	if remote == nil || !remote.ReissueSupported {
		return false
	}
	if validUntil == nil || contractValidUntil == nil {
		return false
	}
	// Treat the order as a long-running contract only when it clearly outlives the current certificate.
	return contractValidUntil.After(validUntil.Add(24 * time.Hour))
}

func readyForDownload(cert *api.TLSCertificate, action string, previousValidUntil *time.Time) bool {
	if !cert.CertificateAvailable {
		return false
	}

	if action != "reissue" {
		return cert.Status == "issued" || cert.Status == "expired"
	}

	if cert.Reissue != nil {
		switch cert.Reissue.Status {
		case "pending", "processing", "pending_approval":
			return false
		case "issued":
			return cert.Status == "issued" || cert.Status == "expired"
		}
	}

	if previousValidUntil == nil || cert.ValidUntil == nil {
		return cert.Status == "issued" || cert.Status == "expired"
	}
	return cert.ValidUntil.After(*previousValidUntil) && (cert.Status == "issued" || cert.Status == "expired")
}

func failedCertificateStatus(status string) bool {
	switch status {
	case "cancelled", "order_cancelled", "rejected", "unknown":
		return true
	default:
		return false
	}
}

func failedReissueStatus(status string) bool {
	switch status {
	case "cancelled", "rejected", "failed":
		return true
	default:
		return false
	}
}

func isActiveReissue(status string) bool {
	switch status {
	case "pending", "processing", "pending_approval":
		return true
	default:
		return false
	}
}

func pickTime(primary, fallback *time.Time) *time.Time {
	if primary != nil {
		return primary
	}
	return fallback
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
