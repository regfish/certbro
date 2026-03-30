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
	CertificatesDir             string
	Progress                    progressReporter
	provisionedValidationRecord map[string]struct{}
}

// OperationResult summarizes the effect of a single issue, import, or renewal action.
type OperationResult struct {
	Name                  string
	Action                string
	Changed               bool
	CertificateID         string
	Status                string
	LiveDir               string
	Message               string
	ActionRequired        bool
	PendingReason         string
	PendingMessage        string
	CompletionURL         string
	PurchasedValidityDays int
	EffectiveValidityDays int
	RenewalBonusDays      int
	HasEffectiveValidity  bool
	HasRenewalBonus       bool
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

const renewalCooldownAfterIssue = 48 * time.Hour

// NewManager constructs a Manager bound to one API client and one local store.
func NewManager(client *api.Client, store *config.Store, storePath string) *Manager {
	return &Manager{
		Client:                      client,
		Store:                       store,
		StorePath:                   storePath,
		CertificatesDir:             config.DefaultCertificatesDir,
		Progress:                    nopProgressReporter{},
		provisionedValidationRecord: make(map[string]struct{}),
	}
}

type waitTimeoutError struct {
	CertificateID string
	Status        string
}

type actionRequiredError struct {
	Certificate *api.TLSCertificate
}

type providerValidationPendingError struct {
	Certificate           *api.TLSCertificate
	ValidationProvisioned bool
}

func (e *waitTimeoutError) Error() string {
	return fmt.Sprintf("timeout waiting for certificate %s, last status %s", e.CertificateID, emptyFallback(e.Status, "unknown"))
}

func (e *actionRequiredError) Error() string {
	if e == nil || e.Certificate == nil {
		return "certificate requires completion in the regfish Console before issuance can continue"
	}
	return fmt.Sprintf("certificate %s requires completion in the regfish Console before issuance can continue", emptyFallback(e.Certificate.ID, "unknown"))
}

func (e *providerValidationPendingError) Error() string {
	if e == nil || e.Certificate == nil {
		return "provider-side OV/business validation is still pending after DCV provisioning"
	}
	return fmt.Sprintf("certificate %s is still pending after DCV provisioning because provider-side OV/business validation is not complete yet", emptyFallback(e.Certificate.ID, "unknown"))
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

	remote, err := m.Client.GetCertificate(ctx, managed.CertificateID)
	if err != nil {
		return nil, err
	}
	if remote.Status == "pending" || (remote.Reissue != nil && isActiveReissue(remote.Reissue.Status)) {
		return nil, errors.New("importing pending certificates is not supported yet; wait until the certificate has been issued")
	}

	managed.CertificateID = remote.ID
	managed.CommonName = remote.CommonName
	if strings.TrimSpace(managed.OutputDir) == "" {
		managed.OutputDir = defaultManagedOutputDir(m.CertificatesDir, remote.CommonName)
	}
	absOutputDir, err := filepath.Abs(managed.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("resolve output dir %s: %w", managed.OutputDir, err)
	}
	managed.OutputDir = absOutputDir
	managed.DNSNames = certcrypto.NormalizeDNSNames("", remote.DNSNames)
	managed.Product = remote.Product
	if !remote.OrganizationID.IsZero() {
		managed.OrganizationID = remote.OrganizationID
	}
	if remote.ValidityDays > 0 {
		managed.ValidityDays = remote.ValidityDays
	}
	if strings.TrimSpace(managed.Name) == "" {
		managed.Name = strings.ToLower(trimCommonName(remote.CommonName))
	}
	if err := config.ValidateRenewalTiming(managed.ValidityDays, managed.RenewBeforeDays, managed.ReissueLeadDays); err != nil {
		return nil, err
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
	populateIssuedValidityDetails(result, effectiveBaseValidityDays(remote.ValidityDays, managed.ValidityDays), remote.ValidFrom, remote.ValidUntil)

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
		ValidityDays:       effectiveBaseValidityDays(remote.ValidityDays, managed.ValidityDays),
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

	action := renewalSkip
	if managed.PendingAction != "" || (remote != nil && (remote.Status == "pending" || (remote.Reissue != nil && isActiveReissue(remote.Reissue.Status)))) {
		action = renewalCompletePending
	} else {
		storedValidityDays := managed.ValidityDays
		if validityDaysOverride == 0 {
			effectiveValidityDays, adjusted, officialEffectiveFrom := config.NormalizeStoredValidityDaysAt(managed.ValidityDays, now)
			if adjusted {
				managed.ValidityDays = effectiveValidityDays
				m.progress().Stepf("%s: stored validity_days %d exceeds the current schedule-aware limit; using %d days for the CA/B Forum limit effective %s", managed.Name, storedValidityDays, effectiveValidityDays, officialEffectiveFrom.Format("2006-01-02"))
			}
		}

		originalRenewBeforeDays := managed.RenewBeforeDays
		originalReissueLeadDays := managed.ReissueLeadDays
		effectiveRenewBeforeDays, effectiveReissueLeadDays, adjustedTiming, err := config.NormalizeStoredRenewalTiming(managed.ValidityDays, managed.RenewBeforeDays, managed.ReissueLeadDays)
		if err != nil {
			return nil, err
		}
		if adjustedTiming {
			managed.RenewBeforeDays = effectiveRenewBeforeDays
			managed.ReissueLeadDays = effectiveReissueLeadDays
			if managed.RenewBeforeDays != originalRenewBeforeDays {
				m.progress().Stepf("%s: stored renew_before_days %d is too large for validity_days %d; using %d instead", managed.Name, originalRenewBeforeDays, managed.ValidityDays, managed.RenewBeforeDays)
			}
			if managed.ReissueLeadDays != originalReissueLeadDays {
				m.progress().Stepf("%s: stored reissue_lead_days %d is too large for validity_days %d; using %d instead", managed.Name, originalReissueLeadDays, managed.ValidityDays, managed.ReissueLeadDays)
			}
		}

		action = planRenewal(now, *managed, remote, force)
	}
	if validityDaysOverride > 0 {
		switch action {
		case renewalOrder, renewalNewOrder:
			if err := config.ValidateRenewalTiming(validityDaysOverride, managed.RenewBeforeDays, managed.ReissueLeadDays); err != nil {
				return nil, err
			}
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
		pendingMeta := pendingMetadataForCertificate(pendingMaterial.Metadata, remote, managed, nil)
		if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
			PrivateKeyPEM: pendingMaterial.PrivateKeyPEM,
			CSRPEM:        pendingMaterial.CSRPEM,
			Metadata:      pendingMeta,
		}); err != nil {
			return nil, err
		}
		if remote.ActionRequired {
			m.applyRemoteState(managed, remote)
			if err := m.persistManaged(*managed); err != nil {
				return nil, err
			}
			return pendingActionRequiredResult(*managed, string(renewalCompletePending), remote, pendingMeta), nil
		}
		initialValidationProvisioned, err := m.ensureValidationRecordsWithChange(ctx, remote)
		if err != nil {
			return nil, err
		}
		m.progress().WaitStart("%s: resuming pending %s for certificate_id %s, still waiting for issuance and this can take a few minutes", managed.Name, pendingAction, remote.ID)
		finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, remote.ID, pendingAction, managed.ValidUntil, waitTimeout, waitInterval, pendingOrderMayPauseAfterValidation(pendingMeta, managed, remote))
		if err != nil {
			var actionErr *actionRequiredError
			if errors.As(err, &actionErr) && actionErr.Certificate != nil {
				pendingMeta = pendingMetadataForCertificate(pendingMeta, actionErr.Certificate, managed, nil)
				if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
					PrivateKeyPEM: pendingMaterial.PrivateKeyPEM,
					CSRPEM:        pendingMaterial.CSRPEM,
					Metadata:      pendingMeta,
				}); err != nil {
					return nil, err
				}
				m.applyRemoteState(managed, actionErr.Certificate)
				if err := m.persistManaged(*managed); err != nil {
					return nil, err
				}
				return pendingActionRequiredResult(*managed, string(renewalCompletePending), actionErr.Certificate, pendingMeta), nil
			}
			var validationPendingErr *providerValidationPendingError
			if errors.As(err, &validationPendingErr) && validationPendingErr.Certificate != nil {
				pendingMeta = pendingMetadataForCertificate(pendingMeta, validationPendingErr.Certificate, managed, nil)
				if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
					PrivateKeyPEM: pendingMaterial.PrivateKeyPEM,
					CSRPEM:        pendingMaterial.CSRPEM,
					Metadata:      pendingMeta,
				}); err != nil {
					return nil, err
				}
				m.applyRemoteState(managed, validationPendingErr.Certificate)
				if err := m.persistManaged(*managed); err != nil {
					return nil, err
				}
				return pendingProviderValidationResult(*managed, string(renewalCompletePending), validationPendingErr.Certificate, initialValidationProvisioned || validationPendingErr.ValidationProvisioned), nil
			}
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
	if err := config.ValidateRenewalTiming(managed.ValidityDays, managed.RenewBeforeDays, managed.ReissueLeadDays); err != nil {
		return nil, err
	}

	m.progress().Stepf("%s: validating TLS product %s", managed.Name, managed.Product)
	product, err := m.resolveProduct(ctx, managed.Product)
	if err != nil {
		return nil, err
	}
	managed.Product = product.SKU
	if productMayRequireConsoleCompletion(product) {
		m.progress().Stepf("%s: %s may require a completion step in the regfish Console before issuance can continue", managed.Name, managed.Product)
	}

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
		Action:                 action,
		CommonName:             managed.CommonName,
		DNSNames:               managed.DNSNames,
		Product:                managed.Product,
		ProductValidationLevel: strings.TrimSpace(product.ValidationLevel),
		OrganizationRequired:   product.OrganizationRequired,
		RequestedAt:            time.Now().UTC(),
		RequestedValidityDays:  managed.ValidityDays,
		OrganizationID:         managed.OrganizationID,
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
		m.progress().Stepf("%s: starting renewal order for certificate_id %s with purchased base validity %d days", managed.Name, renewalOfCertificateID, managed.ValidityDays)
	default:
		m.progress().Stepf("%s: starting certificate order with purchased base validity %d days", managed.Name, managed.ValidityDays)
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

	pendingMeta = pendingMetadataForCertificate(pendingMeta, cert, managed, &product)
	if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
		PrivateKeyPEM: material.PrivateKeyPEM,
		CSRPEM:        material.CSRPEM,
		Metadata:      pendingMeta,
	}); err != nil {
		return nil, err
	}

	if cert.ActionRequired {
		m.progress().Stepf("%s: order started and now requires completion in the regfish Console before issuance can continue", managed.Name)
		return pendingActionRequiredResult(*managed, action, cert, pendingMeta), nil
	}

	if err := m.ensureValidationRecords(ctx, cert); err != nil {
		return nil, err
	}

	m.progress().WaitStart("%s: waiting for issuance of certificate_id %s, this can take a few minutes", managed.Name, cert.ID)
	finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, cert.ID, action, nil, waitTimeout, waitInterval, false)
	if err != nil {
		var actionErr *actionRequiredError
		if errors.As(err, &actionErr) && actionErr.Certificate != nil {
			pendingMeta = pendingMetadataForCertificate(pendingMeta, actionErr.Certificate, managed, &product)
			if err := deploy.WritePending(managed.OutputDir, deploy.PendingMaterial{
				PrivateKeyPEM: material.PrivateKeyPEM,
				CSRPEM:        material.CSRPEM,
				Metadata:      pendingMeta,
			}); err != nil {
				return nil, err
			}
			m.applyRemoteState(managed, actionErr.Certificate)
			if err := m.persistManaged(*managed); err != nil {
				return nil, err
			}
			return pendingActionRequiredResult(*managed, action, actionErr.Certificate, pendingMeta), nil
		}
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
	product, err := m.resolveProduct(ctx, wanted)
	if err != nil {
		return "", err
	}
	return product.SKU, nil
}

// resolveProduct looks up one product entry from the live TLS product catalog.
func (m *Manager) resolveProduct(ctx context.Context, wanted string) (api.TLSProduct, error) {
	products, err := m.Client.ListTLSProducts(ctx)
	if err != nil {
		return api.TLSProduct{}, fmt.Errorf("list TLS products: %w", err)
	}

	return m.resolveProductFromCatalog(products, wanted)
}

// resolveProductSKUFromCatalog validates a product against a previously fetched product catalog.
func (m *Manager) resolveProductSKUFromCatalog(products []api.TLSProduct, wanted string) (string, error) {
	product, err := m.resolveProductFromCatalog(products, wanted)
	if err != nil {
		return "", err
	}
	return product.SKU, nil
}

// resolveProductFromCatalog validates a product against a previously fetched product catalog.
func (m *Manager) resolveProductFromCatalog(products []api.TLSProduct, wanted string) (api.TLSProduct, error) {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return api.TLSProduct{}, errors.New("missing TLS product")
	}

	for _, product := range products {
		if strings.EqualFold(product.SKU, wanted) {
			return product, nil
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
		return api.TLSProduct{}, fmt.Errorf("unknown TLS product %q and the product catalog is empty", wanted)
	}
	return api.TLSProduct{}, fmt.Errorf("unknown TLS product %q; available products: %s", wanted, strings.Join(available, ", "))
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
		Action:                "reissue",
		CertificateID:         remote.ID,
		CommonName:            managed.CommonName,
		DNSNames:              managed.DNSNames,
		Product:               managed.Product,
		RequestedAt:           time.Now().UTC(),
		RequestedValidityDays: managed.ValidityDays,
		OrganizationID:        managed.OrganizationID,
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
	finalCert, fullChainPEM, bundleZIP, err := m.waitAndDownload(ctx, remote.ID, "reissue", remote.ValidUntil, waitTimeout, waitInterval, false)
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
		ValidityDays:       effectiveBaseValidityDays(cert.ValidityDays, managed.ValidityDays),
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

	result := &OperationResult{
		Name:          managed.Name,
		Action:        action,
		Changed:       true,
		CertificateID: cert.ID,
		Status:        cert.Status,
		LiveDir:       deployResult.LiveDir,
		Message:       message,
	}
	populateIssuedValidityDetails(result, effectiveBaseValidityDays(cert.ValidityDays, managed.ValidityDays), cert.ValidFrom, cert.ValidUntil)
	return result, nil
}

func pendingMetadataForCertificate(meta deploy.PendingMetadata, cert *api.TLSCertificate, managed *config.ManagedCertificate, product *api.TLSProduct) deploy.PendingMetadata {
	if cert == nil {
		return meta
	}

	if strings.TrimSpace(cert.ID) != "" {
		meta.CertificateID = cert.ID
	}
	if strings.TrimSpace(cert.CommonName) != "" {
		meta.CommonName = cert.CommonName
	}
	if len(cert.DNSNames) > 0 {
		meta.DNSNames = certcrypto.NormalizeDNSNames("", cert.DNSNames)
	}
	if strings.TrimSpace(cert.Product) != "" {
		meta.Product = cert.Product
	} else if managed != nil && strings.TrimSpace(meta.Product) == "" {
		meta.Product = managed.Product
	}
	if product != nil {
		meta.ProductValidationLevel = strings.TrimSpace(product.ValidationLevel)
		meta.OrganizationRequired = product.OrganizationRequired
	}
	if cert.ValidityDays > 0 {
		meta.RequestedValidityDays = cert.ValidityDays
	} else if meta.RequestedValidityDays == 0 && managed != nil {
		meta.RequestedValidityDays = managed.ValidityDays
	}
	if !cert.OrganizationID.IsZero() {
		meta.OrganizationID = cert.OrganizationID
	} else if meta.OrganizationID.IsZero() && managed != nil {
		meta.OrganizationID = managed.OrganizationID
	}

	meta.ActionRequired = cert.ActionRequired
	if cert.ActionRequired {
		if strings.TrimSpace(cert.PendingReason) != "" {
			meta.PendingReason = strings.TrimSpace(cert.PendingReason)
		} else if meta.PendingReason == "" && product != nil && product.OrganizationRequired {
			meta.PendingReason = "organization_required"
		}
		if strings.TrimSpace(cert.PendingMessage) != "" {
			meta.PendingMessage = strings.TrimSpace(cert.PendingMessage)
		} else if strings.TrimSpace(meta.PendingMessage) == "" {
			meta.PendingMessage = defaultPendingMessage(cert, product)
		}
		if strings.TrimSpace(cert.CompletionURL) != "" {
			meta.CompletionURL = strings.TrimSpace(cert.CompletionURL)
		}
	} else {
		meta.PendingReason = strings.TrimSpace(cert.PendingReason)
		meta.PendingMessage = strings.TrimSpace(cert.PendingMessage)
		meta.CompletionURL = strings.TrimSpace(cert.CompletionURL)
	}

	return meta
}

func defaultPendingMessage(cert *api.TLSCertificate, product *api.TLSProduct) string {
	if product != nil && product.OrganizationRequired {
		return "Complete the OV/business order in the regfish Console. certbro renew will resume issuance afterwards."
	}
	if cert != nil && strings.EqualFold(strings.TrimSpace(cert.PendingReason), "organization_required") {
		return "Complete the organization and contact step in the regfish Console. certbro renew will resume issuance afterwards."
	}
	return "Complete the pending order in the regfish Console. certbro renew will resume issuance afterwards."
}

func productMayRequireConsoleCompletion(product api.TLSProduct) bool {
	if product.OrganizationRequired {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(product.ValidationLevel)) {
	case "ov", "ev":
		return true
	default:
		return false
	}
}

func pendingActionRequiredResult(managed config.ManagedCertificate, action string, cert *api.TLSCertificate, pendingMeta deploy.PendingMetadata) *OperationResult {
	message := "order requires completion in the regfish Console before certbro renew can finish it"
	switch action {
	case "issue":
		message = "order started; complete it in the regfish Console before certbro renew can finish it"
	case string(renewalCompletePending):
		message = "waiting for order completion in the regfish Console"
	}

	result := &OperationResult{
		Name:           managed.Name,
		Action:         action,
		Changed:        false,
		CertificateID:  firstNonEmpty(strings.TrimSpace(pendingMeta.CertificateID), managed.CertificateID),
		Message:        message,
		ActionRequired: true,
		PendingReason:  strings.TrimSpace(pendingMeta.PendingReason),
		PendingMessage: strings.TrimSpace(pendingMeta.PendingMessage),
		CompletionURL:  strings.TrimSpace(pendingMeta.CompletionURL),
	}
	if cert != nil {
		result.Status = cert.Status
		if result.CertificateID == "" {
			result.CertificateID = cert.ID
		}
	}
	if purchasedValidityDays := effectiveBaseValidityDays(0, pendingMeta.RequestedValidityDays); purchasedValidityDays > 0 {
		result.PurchasedValidityDays = purchasedValidityDays
	}
	return result
}

func pendingProviderValidationResult(managed config.ManagedCertificate, action string, cert *api.TLSCertificate, changed bool) *OperationResult {
	message := "provider-side OV/business validation is still pending; certbro renew will continue later"
	if changed {
		message = "DCV provisioned; provider-side OV/business validation is still pending and certbro renew will continue later"
	}

	result := &OperationResult{
		Name:          managed.Name,
		Action:        action,
		Changed:       changed,
		CertificateID: managed.CertificateID,
		Message:       message,
	}
	if cert != nil {
		result.Status = cert.Status
		if result.CertificateID == "" {
			result.CertificateID = cert.ID
		}
	}
	return result
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
	_, err := m.ensureValidationRecordsWithChange(ctx, cert)
	return err
}

func (m *Manager) ensureValidationRecordsWithChange(ctx context.Context, cert *api.TLSCertificate) (bool, error) {
	if m.provisionedValidationRecord == nil {
		m.provisionedValidationRecord = make(map[string]struct{})
	}

	seen := map[string]struct{}{}
	records := make([]api.TLSValidationDNSRecord, 0)
	changed := false

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
			return changed, err
		}
		m.provisionedValidationRecord[key] = struct{}{}
		changed = true
	}

	return changed, nil
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

func (m *Manager) waitAndDownload(ctx context.Context, certificateID, action string, previousValidUntil *time.Time, waitTimeout, waitInterval time.Duration, stopAfterValidationPending bool) (*api.TLSCertificate, []byte, []byte, error) {
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
		validationProvisioned, err := m.ensureValidationRecordsWithChange(ctx, cert)
		if err != nil {
			return nil, nil, nil, err
		}
		if cert.ActionRequired {
			m.progress().WaitDone("certificate_id %s now requires completion in the regfish Console before issuance can continue", certificateID)
			return nil, nil, nil, &actionRequiredError{Certificate: cert}
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
		if stopAfterValidationPending && (validationProvisioned || certificateHasValidationDNSRecords(cert)) {
			m.progress().WaitDone("certificate_id %s is still pending after DCV provisioning; provider-side OV/business validation can continue asynchronously", certificateID)
			return nil, nil, nil, &providerValidationPendingError{
				Certificate:           cert,
				ValidationProvisioned: validationProvisioned,
			}
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

func pendingOrderMayPauseAfterValidation(meta deploy.PendingMetadata, managed *config.ManagedCertificate, cert *api.TLSCertificate) bool {
	if meta.OrganizationRequired {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(meta.ProductValidationLevel)) {
	case "ov", "ev":
		return true
	}
	if cert != nil {
		if cert.ActionRequired || strings.EqualFold(strings.TrimSpace(cert.PendingReason), "organization_required") {
			return true
		}
		if !cert.OrganizationID.IsZero() || (cert.Organization != nil && !cert.Organization.ID.IsZero()) {
			return true
		}
	}
	if !meta.OrganizationID.IsZero() {
		return true
	}
	return managed != nil && !managed.OrganizationID.IsZero()
}

func certificateHasValidationDNSRecords(cert *api.TLSCertificate) bool {
	if cert == nil {
		return false
	}
	if cert.Validation != nil && cert.Validation.Method == "dns-cname-token" && len(cert.Validation.DNSRecords) > 0 {
		return true
	}
	if cert.Reissue != nil && cert.Reissue.Validation != nil && cert.Reissue.Validation.Method == "dns-cname-token" && len(cert.Reissue.Validation.DNSRecords) > 0 {
		return true
	}
	return false
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
	if !cert.OrganizationID.IsZero() {
		managed.OrganizationID = cert.OrganizationID
	}
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
	validFrom := pickTime(remote.ValidFrom, managed.ValidFrom)
	issuedAt := pickTime(managed.LastIssuedAt, validFrom)
	preferReissue := shouldPreferReissue(remote, validUntil, contractValidUntil)
	leadDays := managed.RenewBeforeDays
	if preferReissue {
		leadDays = managed.ReissueLeadDays
	}

	if !force && withinRenewalCooldown(now, issuedAt, renewalCooldownAfterIssue) {
		return renewalSkip
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

func withinRenewalCooldown(now time.Time, issuedAt *time.Time, cooldown time.Duration) bool {
	if issuedAt == nil || cooldown <= 0 {
		return false
	}
	return now.Before(issuedAt.Add(cooldown))
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

func effectiveBaseValidityDays(preferred, fallback int) int {
	if preferred > 0 {
		return preferred
	}
	return fallback
}

func populateIssuedValidityDetails(result *OperationResult, purchasedValidityDays int, validFrom, validUntil *time.Time) {
	if result == nil {
		return
	}
	if purchasedValidityDays > 0 {
		result.PurchasedValidityDays = purchasedValidityDays
	}
	if effectiveValidityDays, ok := config.EffectiveIssuedValidityDays(validFrom, validUntil); ok {
		result.EffectiveValidityDays = effectiveValidityDays
		result.HasEffectiveValidity = true
	}
	if renewalBonusDays, ok := config.ConfirmedRenewalBonusDays(purchasedValidityDays, validFrom, validUntil); ok {
		if renewalBonusDays > 0 {
			result.RenewalBonusDays = renewalBonusDays
			result.HasRenewalBonus = true
		}
	}
}
