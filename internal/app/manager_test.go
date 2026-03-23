// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/regfish/certbro/internal/api"
	"github.com/regfish/certbro/internal/config"
	"github.com/regfish/certbro/internal/deploy"
	"github.com/regfish/certbro/internal/testutil"
)

type recordingProgressReporter struct {
	steps []string
}

func (r *recordingProgressReporter) Stepf(format string, args ...any) {
	r.steps = append(r.steps, fmt.Sprintf(format, args...))
}

func (r *recordingProgressReporter) WaitStart(format string, args ...any) {
	r.steps = append(r.steps, fmt.Sprintf(format, args...))
}

func (r *recordingProgressReporter) WaitTick(format string, args ...any) {
	r.steps = append(r.steps, fmt.Sprintf(format, args...))
}

func (r *recordingProgressReporter) WaitDone(format string, args ...any) {
	r.steps = append(r.steps, fmt.Sprintf(format, args...))
}

func containsProgressStep(steps []string, want string) bool {
	for _, step := range steps {
		if strings.Contains(step, want) {
			return true
		}
	}
	return false
}

func TestPlanRenewal(t *testing.T) {
	now := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)

	farValidUntil := now.Add(30 * 24 * time.Hour)
	nearValidUntil := now.Add(3 * 24 * time.Hour)
	equalContractEnd := nearValidUntil
	longContractEnd := now.Add(120 * 24 * time.Hour)
	recentIssuedAt := now.Add(-24 * time.Hour)

	tests := []struct {
		name    string
		managed config.ManagedCertificate
		remote  *api.TLSCertificate
		force   bool
		want    renewalAction
	}{
		{
			name: "pending action resumes pending workflow",
			managed: config.ManagedCertificate{
				PendingAction:   "issue",
				ReissueLeadDays: 7,
				RenewBeforeDays: 30,
			},
			remote: &api.TLSCertificate{
				Status: "pending",
			},
			want: renewalCompletePending,
		},
		{
			name: "missing remote cert triggers new order",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			want: renewalNewOrder,
		},
		{
			name: "far from expiry skips renewal",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &farValidUntil,
				ContractValidUntil: &longContractEnd,
			},
			want: renewalSkip,
		},
		{
			name: "near expiry with equal contract renews order",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &nearValidUntil,
				ContractValidUntil: &equalContractEnd,
			},
			want: renewalOrder,
		},
		{
			name: "near expiry with longer contract reissues",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &nearValidUntil,
				ContractValidUntil: &longContractEnd,
			},
			want: renewalReissue,
		},
		{
			name: "force prefers renewal order for equal contract",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &farValidUntil,
				ContractValidUntil: &farValidUntil,
			},
			force: true,
			want:  renewalOrder,
		},
		{
			name: "force prefers reissue when contract clearly outlives certificate",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &farValidUntil,
				ContractValidUntil: &longContractEnd,
			},
			force: true,
			want:  renewalReissue,
		},
		{
			name: "near expiry without renewal support falls back to new order",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   false,
				Status:             "issued",
				ReissueSupported:   false,
				ValidUntil:         &nearValidUntil,
				ContractValidUntil: &equalContractEnd,
			},
			want: renewalNewOrder,
		},
		{
			name: "recently issued certificate stays on cooldown",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
				LastIssuedAt:    &recentIssuedAt,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &nearValidUntil,
				ContractValidUntil: &equalContractEnd,
			},
			want: renewalSkip,
		},
		{
			name: "force bypasses recent issuance cooldown",
			managed: config.ManagedCertificate{
				ReissueLeadDays: 7,
				RenewBeforeDays: 7,
				LastIssuedAt:    &recentIssuedAt,
			},
			remote: &api.TLSCertificate{
				ID:                 "7K9QW3M2ZT8HJ",
				RenewalSupported:   true,
				Status:             "issued",
				ReissueSupported:   true,
				ValidUntil:         &nearValidUntil,
				ContractValidUntil: &equalContractEnd,
			},
			force: true,
			want:  renewalOrder,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := planRenewal(now, tc.managed, tc.remote, tc.force)
			if got != tc.want {
				t.Fatalf("planRenewal() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestManagerImportStoresManagedCertificate(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/tls/certificate/7K9QW3M2ZT8HJ" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"response": {
				"id": "7K9QW3M2ZT8HJ",
				"status": "issued",
				"common_name": "example.com",
				"product": "RapidSSL",
				"provider": "digicert",
				"dns_names": ["www.example.com"],
				"order_state": "ISSUED",
				"reissue_supported": true,
				"validity_days": 199,
				"certificate_pem_available": true
			}
		}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	outputDir := filepath.Join(root, "example.com")
	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, statePath)
	manager.CertificatesDir = root

	result, err := manager.Import(context.Background(), config.ManagedCertificate{
		Name:          "example-com",
		CertificateID: "7K9QW3M2ZT8HJ",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Message != "certificate imported for renewal management" {
		t.Fatalf("result.Message = %q", result.Message)
	}

	stored, idx := manager.Store.FindManagedCertificate("example-com")
	if idx < 0 || stored == nil {
		t.Fatal("managed certificate not stored")
	}
	if stored.CertificateID != "7K9QW3M2ZT8HJ" || stored.CommonName != "example.com" || stored.Product != "RapidSSL" {
		t.Fatalf("stored managed certificate = %#v", *stored)
	}
	if stored.OutputDir != outputDir {
		t.Fatalf("stored.OutputDir = %q, want %q", stored.OutputDir, outputDir)
	}
}

func TestManagerImportRejectsPendingCertificates(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"response": {
				"id": "7K9QW3M2ZT8HJ",
				"status": "pending",
				"common_name": "example.com",
				"product": "RapidSSL",
				"provider": "digicert",
				"dns_names": ["www.example.com"],
				"order_state": "PENDING",
				"certificate_pem_available": false
			}
		}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	_, err = manager.Import(context.Background(), config.ManagedCertificate{
		Name:          "example-com",
		OutputDir:     filepath.Join(t.TempDir(), "example.com"),
		CertificateID: "7K9QW3M2ZT8HJ",
	}, nil, nil)
	if err == nil {
		t.Fatal("Import() error = nil, want error")
	}
}

func TestResolveProductSKUAcceptsCaseInsensitiveMatch(t *testing.T) {
	manager := &Manager{}
	got, err := manager.resolveProductSKUFromCatalog([]api.TLSProduct{
		{SKU: "RapidSSL"},
		{SKU: "SecureSite"},
	}, "rapidssl")
	if err != nil {
		t.Fatalf("resolveProductSKUFromCatalog() error = %v", err)
	}
	if got != "RapidSSL" {
		t.Fatalf("resolveProductSKUFromCatalog() = %q, want RapidSSL", got)
	}
}

func TestResolveProductSKUFromCatalogListsAvailableProducts(t *testing.T) {
	manager := &Manager{}
	_, err := manager.resolveProductSKUFromCatalog([]api.TLSProduct{
		{SKU: "RapidSSL"},
		{SKU: "SecureSite"},
		{SKU: "SSL123"},
	}, "NopeSSL")
	if err == nil {
		t.Fatal("resolveProductSKUFromCatalog() error = nil, want error")
	}
	for _, want := range []string{"NopeSSL", "RapidSSL", "SecureSite", "SSL123"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestStartOrderRejectsUnknownProductBeforeOrdering(t *testing.T) {
	var createCalled bool
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{"sku": "RapidSSL", "name": "RapidSSL", "type": "dv", "ca": "digicert"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			createCalled = true
			http.Error(w, "should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	_, err = manager.startOrder(context.Background(), &config.ManagedCertificate{
		Name:       "example-com",
		CommonName: "example.com",
		Product:    "NopeSSL",
		OutputDir:  filepath.Join(t.TempDir(), "example.com"),
	}, time.Minute, time.Second, "issue", "")
	if err == nil {
		t.Fatal("startOrder() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown TLS product") {
		t.Fatalf("startOrder() error = %v, want product error", err)
	}
	if createCalled {
		t.Fatal("CreateCertificate endpoint was called for an invalid product")
	}
}

func TestRenewOneAppliesValidityOverrideToRenewalOrder(t *testing.T) {
	oldValidUntil := time.Now().UTC().Add(60 * 24 * time.Hour)
	newValidUntil := time.Now().UTC().Add(90 * 24 * time.Hour)
	var gotValidityDays int
	var gotRenewalOf string

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/OLDCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "OLDCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"certificate_pem_available": true,
					"valid_until": "` + oldValidUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + oldValidUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{"sku": "RapidSSL", "name": "RapidSSL", "type": "dv", "ca": "digicert"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			var req api.TLSCertificateRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			gotValidityDays = req.ValidityDays
			gotRenewalOf = req.RenewalOfCertificateID

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "pending",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "PENDING",
					"certificate_pem_available": false
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"certificate_pem_available": true,
					"valid_until": "` + newValidUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + newValidUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/zip":
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:            "example-com",
		CommonName:      "example.com",
		Product:         "RapidSSL",
		OutputDir:       outputDir,
		CertificateID:   "OLDCERT",
		ValidityDays:    199,
		RenewBeforeDays: 2,
		ReissueLeadDays: 2,
	}, true, 3, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if result.Action != "renewal-order" {
		t.Fatalf("result.Action = %q, want renewal-order", result.Action)
	}
	if gotValidityDays != 3 {
		t.Fatalf("CreateCertificate validity_days = %d, want 3", gotValidityDays)
	}
	if gotRenewalOf != "OLDCERT" {
		t.Fatalf("CreateCertificate renewal_of_certificate_id = %q, want OLDCERT", gotRenewalOf)
	}
}

func TestStartOrderSendsPurchasedBaseValidityForFreshOrder(t *testing.T) {
	validFrom := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	validUntil := validFrom.Add(30 * 24 * time.Hour)
	var gotValidityDays int
	var gotRenewalOf string

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{"sku": "RapidSSL", "name": "RapidSSL", "type": "dv", "ca": "digicert"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			var req api.TLSCertificateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			gotValidityDays = req.ValidityDays
			gotRenewalOf = req.RenewalOfCertificateID
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "pending",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "PENDING",
					"validity_days": 30,
					"certificate_pem_available": false
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"validity_days": 30,
					"certificate_pem_available": true,
					"valid_from": "` + validFrom.Format(time.RFC3339) + `",
					"valid_until": "` + validUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + validUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/zip":
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	result, err := manager.startOrder(context.Background(), &config.ManagedCertificate{
		Name:            "example-com",
		CommonName:      "example.com",
		Product:         "RapidSSL",
		OutputDir:       filepath.Join(t.TempDir(), "example.com"),
		ValidityDays:    30,
		RenewBeforeDays: 7,
		ReissueLeadDays: 7,
	}, time.Minute, time.Millisecond, "issue", "")
	if err != nil {
		t.Fatalf("startOrder() error = %v", err)
	}
	if gotValidityDays != 30 {
		t.Fatalf("CreateCertificate validity_days = %d, want 30", gotValidityDays)
	}
	if gotRenewalOf != "" {
		t.Fatalf("CreateCertificate renewal_of_certificate_id = %q, want empty", gotRenewalOf)
	}
	if result.PurchasedValidityDays != 30 {
		t.Fatalf("result.PurchasedValidityDays = %d, want 30", result.PurchasedValidityDays)
	}
	if !result.HasEffectiveValidity || result.EffectiveValidityDays != 30 {
		t.Fatalf("result effective validity = %d / %v, want 30 / true", result.EffectiveValidityDays, result.HasEffectiveValidity)
	}
}

func TestStartOrderReturnsPendingActionRequiredWithoutWaiting(t *testing.T) {
	var getCertificateCalled bool
	var downloadCalled bool

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{
						"sku": "SecureSite",
						"name": "Secure Site",
						"type": "ov",
						"ca": "digicert",
						"validation_level": "ov",
						"organization_required": true
					}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "OVPENDING",
					"status": "pending",
					"common_name": "example.com",
					"product": "SecureSite",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ACTION_REQUIRED",
					"action_required": true,
					"completion_url": "https://dash.regfish.com/my/certs/OVPENDING/complete",
					"organization_id": 42,
					"validity_days": 199,
					"certificate_pem_available": false
				}
			}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tls/certificate/"):
			getCertificateCalled = true
			http.Error(w, "should not poll staged OV orders", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/download/"):
			downloadCalled = true
			http.Error(w, "should not download staged OV orders", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	progress := &recordingProgressReporter{}
	manager.Progress = progress
	result, err := manager.startOrder(context.Background(), &config.ManagedCertificate{
		Name:            "example-com",
		CommonName:      "example.com",
		Product:         "SecureSite",
		OutputDir:       outputDir,
		ValidityDays:    199,
		RenewBeforeDays: 7,
		ReissueLeadDays: 7,
	}, time.Minute, time.Millisecond, "issue", "")
	if err != nil {
		t.Fatalf("startOrder() error = %v", err)
	}
	if !result.ActionRequired {
		t.Fatalf("result.ActionRequired = %v, want true", result.ActionRequired)
	}
	if result.Action != "issue" {
		t.Fatalf("result.Action = %q, want issue", result.Action)
	}
	if result.CompletionURL != "https://dash.regfish.com/my/certs/OVPENDING/complete" {
		t.Fatalf("result.CompletionURL = %q", result.CompletionURL)
	}
	if result.PendingReason != "organization_required" {
		t.Fatalf("result.PendingReason = %q, want organization_required", result.PendingReason)
	}
	if !strings.Contains(result.PendingMessage, "regfish Console") {
		t.Fatalf("result.PendingMessage = %q, want console guidance", result.PendingMessage)
	}
	if result.PurchasedValidityDays != 199 {
		t.Fatalf("result.PurchasedValidityDays = %d, want 199", result.PurchasedValidityDays)
	}
	if getCertificateCalled || downloadCalled {
		t.Fatalf("poll/download called unexpectedly: get=%v download=%v", getCertificateCalled, downloadCalled)
	}
	if !containsProgressStep(progress.steps, "may require a completion step in the regfish Console") {
		t.Fatalf("progress steps = %#v, want OV/business completion hint", progress.steps)
	}

	pending, err := deploy.LoadPending(outputDir)
	if err != nil {
		t.Fatalf("LoadPending() error = %v", err)
	}
	if !pending.Metadata.ActionRequired {
		t.Fatalf("pending.Metadata.ActionRequired = %v, want true", pending.Metadata.ActionRequired)
	}
	if pending.Metadata.PendingReason != "organization_required" {
		t.Fatalf("pending.Metadata.PendingReason = %q, want organization_required", pending.Metadata.PendingReason)
	}
	if pending.Metadata.CompletionURL != "https://dash.regfish.com/my/certs/OVPENDING/complete" {
		t.Fatalf("pending.Metadata.CompletionURL = %q", pending.Metadata.CompletionURL)
	}
	if pending.Metadata.RequestedValidityDays != 199 {
		t.Fatalf("pending.Metadata.RequestedValidityDays = %d, want 199", pending.Metadata.RequestedValidityDays)
	}

	stored, _ := manager.Store.FindManagedCertificate("example-com")
	if stored == nil {
		t.Fatal("managed certificate not stored")
	}
	if stored.CertificateID != "OVPENDING" || stored.PendingAction != "issue" || stored.OrganizationID != 42 {
		t.Fatalf("stored managed certificate = %#v", *stored)
	}
}

func TestRenewOneKeepsPurchasedBaseValidityForRenewalOrder(t *testing.T) {
	oldValidUntil := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	newValidFrom := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	newValidUntil := newValidFrom.Add(206 * 24 * time.Hour)
	var gotValidityDays int
	var gotRenewalOf string

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/OLDCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "OLDCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"validity_days": 199,
					"certificate_pem_available": true,
					"valid_until": "` + oldValidUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + oldValidUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{"sku": "RapidSSL", "name": "RapidSSL", "type": "dv", "ca": "digicert"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			var req api.TLSCertificateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			gotValidityDays = req.ValidityDays
			gotRenewalOf = req.RenewalOfCertificateID
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "pending",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "PENDING",
					"validity_days": 199,
					"certificate_pem_available": false
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "NEWCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"validity_days": 199,
					"renewal_bonus_days": 7,
					"certificate_pem_available": true,
					"valid_from": "` + newValidFrom.Format(time.RFC3339) + `",
					"valid_until": "` + newValidUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + newValidUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/NEWCERT/download/zip":
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:            "example-com",
		CommonName:      "example.com",
		Product:         "RapidSSL",
		OutputDir:       filepath.Join(t.TempDir(), "example.com"),
		CertificateID:   "OLDCERT",
		ValidityDays:    199,
		RenewBeforeDays: 2,
		ReissueLeadDays: 2,
	}, true, 0, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if gotValidityDays != 199 {
		t.Fatalf("CreateCertificate validity_days = %d, want 199", gotValidityDays)
	}
	if gotRenewalOf != "OLDCERT" {
		t.Fatalf("CreateCertificate renewal_of_certificate_id = %q, want OLDCERT", gotRenewalOf)
	}
	if result.PurchasedValidityDays != 199 {
		t.Fatalf("result.PurchasedValidityDays = %d, want 199", result.PurchasedValidityDays)
	}
	if !result.HasEffectiveValidity || result.EffectiveValidityDays != 206 {
		t.Fatalf("result effective validity = %d / %v, want 206 / true", result.EffectiveValidityDays, result.HasEffectiveValidity)
	}
	if !result.HasRenewalBonus || result.RenewalBonusDays != 7 {
		t.Fatalf("result renewal bonus = %d / %v, want 7 / true", result.RenewalBonusDays, result.HasRenewalBonus)
	}
}

func TestRenewOneKeepsPendingActionRequiredWithoutWaiting(t *testing.T) {
	var createCalled bool
	var downloadCalled bool

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/OVPENDING":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "OVPENDING",
					"status": "pending",
					"common_name": "example.com",
					"product": "SecureSite",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ACTION_REQUIRED",
					"action_required": true,
					"pending_reason": "organization_required",
					"pending_message": "Complete the organization and contact validation in the regfish Console.",
					"completion_url": "https://dash.regfish.com/my/certs/OVPENDING/complete",
					"organization_id": 42,
					"validity_days": 199,
					"certificate_pem_available": false
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			createCalled = true
			http.Error(w, "should not create a new order while action is still required", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/download/"):
			downloadCalled = true
			http.Error(w, "should not download before the order is completed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	if err := deploy.WritePending(outputDir, deploy.PendingMaterial{
		PrivateKeyPEM: []byte("test-key"),
		CSRPEM:        []byte("test-csr"),
		Metadata: deploy.PendingMetadata{
			Action:                "issue",
			CertificateID:         "OVPENDING",
			CommonName:            "example.com",
			Product:               "SecureSite",
			RequestedAt:           time.Now().UTC(),
			RequestedValidityDays: 199,
			ActionRequired:        true,
			PendingReason:         "organization_required",
			CompletionURL:         "https://dash.regfish.com/my/certs/OVPENDING/complete",
		},
	}); err != nil {
		t.Fatalf("WritePending() error = %v", err)
	}

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:          "example-com",
		CommonName:    "example.com",
		Product:       "SecureSite",
		OutputDir:     outputDir,
		CertificateID: "OVPENDING",
		PendingAction: "issue",
		ValidityDays:  199,
	}, false, 0, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if !result.ActionRequired {
		t.Fatalf("result.ActionRequired = %v, want true", result.ActionRequired)
	}
	if result.Action != string(renewalCompletePending) {
		t.Fatalf("result.Action = %q, want %q", result.Action, renewalCompletePending)
	}
	if result.CompletionURL != "https://dash.regfish.com/my/certs/OVPENDING/complete" {
		t.Fatalf("result.CompletionURL = %q", result.CompletionURL)
	}
	if createCalled || downloadCalled {
		t.Fatalf("unexpected order/download activity: create=%v download=%v", createCalled, downloadCalled)
	}
}

func TestRenewOneResumesPendingDVOrderWhenValidationAppearsLater(t *testing.T) {
	validFrom := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	validUntil := validFrom.Add(199 * 24 * time.Hour)
	var dnsCreateCount int
	var getCertificateCount int

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT":
			getCertificateCount++
			w.Header().Set("Content-Type", "application/json")
			if getCertificateCount == 1 {
				_, _ = w.Write([]byte(`{
					"success": true,
					"response": {
						"id": "PENDINGCERT",
						"status": "pending",
						"common_name": "example.com",
						"product": "RapidSSL",
						"provider": "digicert",
						"dns_names": ["example.com"],
						"order_state": "PENDING",
						"action_required": false,
						"validity_days": 199,
						"certificate_pem_available": false,
						"validation": {
							"method": "dns-cname-token",
							"dns_records": [
								{
									"name": "_dnsauth.example.com.",
									"type": "CNAME",
									"value": "_token.dcv.digicert.com."
								}
							]
						}
					}
				}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "PENDINGCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"action_required": false,
					"validity_days": 199,
					"certificate_pem_available": true,
					"valid_from": "` + validFrom.Format(time.RFC3339) + `",
					"valid_until": "` + validUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + validUntil.Format(time.RFC3339) + `",
					"validation": {
						"method": "dns-cname-token",
						"dns_records": [
							{
								"name": "_dnsauth.example.com.",
								"type": "CNAME",
								"value": "_token.dcv.digicert.com."
							}
						]
					}
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/dns/rr":
			dnsCreateCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success": true, "response": {"type":"CNAME","name":"_dnsauth.example.com.","data":"_token.dcv.digicert.com.","ttl":60}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT/download/pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT/download/zip":
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	if err := deploy.WritePending(outputDir, deploy.PendingMaterial{
		PrivateKeyPEM: []byte("test-key"),
		CSRPEM:        []byte("test-csr"),
		Metadata: deploy.PendingMetadata{
			Action:                "issue",
			CertificateID:         "PENDINGCERT",
			CommonName:            "example.com",
			Product:               "RapidSSL",
			RequestedAt:           time.Now().UTC(),
			RequestedValidityDays: 199,
		},
	}); err != nil {
		t.Fatalf("WritePending() error = %v", err)
	}

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:          "example-com",
		CommonName:    "example.com",
		Product:       "RapidSSL",
		OutputDir:     outputDir,
		CertificateID: "PENDINGCERT",
		PendingAction: "issue",
		ValidityDays:  199,
	}, false, 0, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatalf("result.Changed = %v, want true", result.Changed)
	}
	if dnsCreateCount != 1 {
		t.Fatalf("dnsCreateCount = %d, want 1", dnsCreateCount)
	}
	if result.LiveDir == "" {
		t.Fatal("result.LiveDir is empty, want deployed live dir")
	}
}

func TestRenewOneReturnsAfterProvisioningOVValidationWhileProviderApprovalRemainsPending(t *testing.T) {
	var dnsCreateCount int
	var getCertificateCount int

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT":
			getCertificateCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "PENDINGCERT",
					"status": "pending",
					"common_name": "example.com",
					"product": "SecureSite",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "PENDING_APPROVAL",
					"action_required": false,
					"organization_id": 42,
					"validity_days": 199,
					"certificate_pem_available": false,
					"validation": {
						"method": "dns-cname-token",
						"dns_records": [
							{
								"name": "_dnsauth.example.com.",
								"type": "CNAME",
								"value": "_token.dcv.digicert.com."
							}
						]
					}
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/dns/rr":
			dnsCreateCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success": true, "response": {"type":"CNAME","name":"_dnsauth.example.com.","data":"_token.dcv.digicert.com.","ttl":60}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT/download/pem":
			http.Error(w, "should not download while OV approval is still pending", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	if err := deploy.WritePending(outputDir, deploy.PendingMaterial{
		PrivateKeyPEM: []byte("test-key"),
		CSRPEM:        []byte("test-csr"),
		Metadata: deploy.PendingMetadata{
			Action:                 "issue",
			CertificateID:          "PENDINGCERT",
			CommonName:             "example.com",
			Product:                "SecureSite",
			ProductValidationLevel: "ov",
			OrganizationRequired:   true,
			RequestedAt:            time.Now().UTC(),
			RequestedValidityDays:  199,
			OrganizationID:         42,
			ActionRequired:         true,
			PendingReason:          "organization_required",
			PendingMessage:         "Complete the organization and contact validation in the regfish Console.",
			CompletionURL:          "https://dash.regfish.com/my/certs/PENDINGCERT/complete",
		},
	}); err != nil {
		t.Fatalf("WritePending() error = %v", err)
	}

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:           "example-com",
		CommonName:     "example.com",
		Product:        "SecureSite",
		OutputDir:      outputDir,
		CertificateID:  "PENDINGCERT",
		PendingAction:  "issue",
		ValidityDays:   199,
		OrganizationID: 42,
	}, false, 0, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatalf("result.Changed = %v, want true because DCV was provisioned", result.Changed)
	}
	if result.LiveDir != "" {
		t.Fatalf("result.LiveDir = %q, want empty because issuance must not be required immediately", result.LiveDir)
	}
	if result.Message != "DCV provisioned; provider-side OV/business validation is still pending and certbro renew will continue later" {
		t.Fatalf("result.Message = %q", result.Message)
	}
	if dnsCreateCount != 1 {
		t.Fatalf("dnsCreateCount = %d, want 1", dnsCreateCount)
	}
	if getCertificateCount < 2 {
		t.Fatalf("getCertificateCount = %d, want at least 2 because renew should re-check once after DCV appears", getCertificateCount)
	}

	pending, err := deploy.LoadPending(outputDir)
	if err != nil {
		t.Fatalf("LoadPending() error = %v", err)
	}
	if pending.Metadata.ActionRequired {
		t.Fatalf("pending.Metadata.ActionRequired = %v, want false after Console completion", pending.Metadata.ActionRequired)
	}
	if pending.Metadata.CompletionURL != "" {
		t.Fatalf("pending.Metadata.CompletionURL = %q, want cleared completion URL", pending.Metadata.CompletionURL)
	}
}

func TestRenewOneFinalizesAlreadyIssuedPendingOrder(t *testing.T) {
	validFrom := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	validUntil := validFrom.Add(199 * 24 * time.Hour)
	var dnsCreateCount int

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "PENDINGCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "SecureSite",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"action_required": false,
					"organization_id": 42,
					"validity_days": 199,
					"certificate_pem_available": true,
					"valid_from": "` + validFrom.Format(time.RFC3339) + `",
					"valid_until": "` + validUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + validUntil.Format(time.RFC3339) + `"
				}
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/dns/rr":
			dnsCreateCount++
			http.Error(w, "should not create DNS records when no validation is pending", http.StatusInternalServerError)
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT/download/pem":
			w.Header().Set("Content-Type", "application/x-pem-file")
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n-----BEGIN CERTIFICATE-----\nAQ==\n-----END CERTIFICATE-----\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT/download/zip":
			http.Error(w, `{"message":"Not found"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	if err := deploy.WritePending(outputDir, deploy.PendingMaterial{
		PrivateKeyPEM: []byte("test-key"),
		CSRPEM:        []byte("test-csr"),
		Metadata: deploy.PendingMetadata{
			Action:                "issue",
			CertificateID:         "PENDINGCERT",
			CommonName:            "example.com",
			Product:               "SecureSite",
			RequestedAt:           time.Now().UTC(),
			RequestedValidityDays: 199,
			OrganizationID:        42,
		},
	}); err != nil {
		t.Fatalf("WritePending() error = %v", err)
	}

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	result, err := manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:          "example-com",
		CommonName:    "example.com",
		Product:       "SecureSite",
		OutputDir:     outputDir,
		CertificateID: "PENDINGCERT",
		PendingAction: "issue",
		ValidityDays:  199,
	}, false, 0, time.Minute, time.Millisecond)
	if err != nil {
		t.Fatalf("renewOne() error = %v", err)
	}
	if !result.Changed {
		t.Fatalf("result.Changed = %v, want true", result.Changed)
	}
	if dnsCreateCount != 0 {
		t.Fatalf("dnsCreateCount = %d, want 0", dnsCreateCount)
	}
	if _, err := deploy.LoadPending(outputDir); err == nil {
		t.Fatal("LoadPending() error = nil, want pending state to be cleared after deployment")
	}
}

func TestRenewOneRejectsValidityOverrideForReissue(t *testing.T) {
	validUntil := time.Now().UTC().Add(2 * 24 * time.Hour)
	contractValidUntil := time.Now().UTC().Add(90 * 24 * time.Hour)

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/OLDCERT" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "OLDCERT",
					"status": "issued",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "ISSUED",
					"renewal_supported": true,
					"reissue_supported": true,
					"certificate_pem_available": true,
					"valid_until": "` + validUntil.Format(time.RFC3339) + `",
					"contract_valid_until": "` + contractValidUntil.Format(time.RFC3339) + `"
				}
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	_, err = manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:            "example-com",
		CommonName:      "example.com",
		Product:         "RapidSSL",
		OutputDir:       filepath.Join(t.TempDir(), "example.com"),
		CertificateID:   "OLDCERT",
		ValidityDays:    199,
		RenewBeforeDays: 7,
		ReissueLeadDays: 7,
	}, true, 3, time.Minute, time.Millisecond)
	if err == nil {
		t.Fatal("renewOne() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--validity-days cannot be applied to a reissue") {
		t.Fatalf("renewOne() error = %v", err)
	}
}

func TestStartOrderRejectsValidityDaysAboveCurrentLimitBeforeOrdering(t *testing.T) {
	var createCalled bool
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{"sku": "RapidSSL", "name": "RapidSSL", "type": "dv", "ca": "digicert"}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
			createCalled = true
			http.Error(w, "should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	_, err = manager.startOrder(context.Background(), &config.ManagedCertificate{
		Name:         "example-com",
		CommonName:   "example.com",
		Product:      "RapidSSL",
		OutputDir:    filepath.Join(t.TempDir(), "example.com"),
		ValidityDays: 401,
	}, time.Minute, time.Second, "issue", "")
	if err == nil {
		t.Fatal("startOrder() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("startOrder() error = %v, want schedule-limit error", err)
	}
	if createCalled {
		t.Fatal("CreateCertificate endpoint was called for an excessive validity")
	}
}

func TestEnsureValidationRecordsProvisionsEachRecordOnlyOncePerRun(t *testing.T) {
	var listCount int
	var deleteCount int
	var postCount int
	var seenTTLs []int
	var seenAnnotations []string

	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path != "/dns/example.com/rr" {
				http.NotFound(w, r)
				return
			}
			listCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": [
					{
						"id": 10,
						"type": "CNAME",
						"name": "_dnsauth.example.com.",
						"data": "_old-token.dcv.digicert.com.",
						"ttl": 300,
						"annotation": "managed by certbro",
						"auto": false,
						"active": true
					}
				]
			}`))
		case http.MethodDelete:
			if r.URL.Path != "/dns/rr/10" {
				http.NotFound(w, r)
				return
			}
			deleteCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success": true}`))
		case http.MethodPost:
			if r.URL.Path != "/dns/rr" {
				http.NotFound(w, r)
				return
			}
			postCount++
			var record api.DNSRecord
			if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			seenTTLs = append(seenTTLs, record.TTL)
			seenAnnotations = append(seenAnnotations, record.Annotation)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"type": "CNAME",
					"name": "_dnsauth.example.com.",
					"data": "_token.dcv.digicert.com.",
					"ttl": 60
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	cert := &api.TLSCertificate{
		ID: "PENDINGCERT",
		Validation: &api.TLSValidation{
			Method: "dns-cname-token",
			DNSRecords: []api.TLSValidationDNSRecord{
				{
					Name:  "_dnsauth.example.com.",
					Type:  "CNAME",
					Value: "_token.dcv.digicert.com.",
				},
			},
		},
	}

	if err := manager.ensureValidationRecords(context.Background(), cert); err != nil {
		t.Fatalf("ensureValidationRecords() error = %v", err)
	}
	if err := manager.ensureValidationRecords(context.Background(), cert); err != nil {
		t.Fatalf("ensureValidationRecords() second error = %v", err)
	}

	if listCount != 1 {
		t.Fatalf("GET count = %d, want 1", listCount)
	}
	if deleteCount != 1 {
		t.Fatalf("DELETE count = %d, want 1", deleteCount)
	}
	if postCount != 1 {
		t.Fatalf("POST count = %d, want 1", postCount)
	}
	for _, ttl := range seenTTLs {
		if ttl != 60 {
			t.Fatalf("saw TTL %d, want 60", ttl)
		}
	}
	for _, annotation := range seenAnnotations {
		if annotation != "managed by certbro; certificate_id=PENDINGCERT" {
			t.Fatalf("saw annotation %q, want certificate-scoped certbro annotation", annotation)
		}
	}
}

func TestEnsureValidationRecordsRejectsForeignConflictingCNAME(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/dns/example.com/rr" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"response": [
				{
					"id": 12,
					"type": "CNAME",
					"name": "_dnsauth.example.com.",
					"data": "_foreign-token.dcv.digicert.com.",
					"ttl": 60,
					"annotation": "set manually",
					"auto": false,
					"active": true
				}
			]
		}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(t.TempDir(), "state.json"))
	err = manager.ensureValidationRecords(context.Background(), &api.TLSCertificate{
		ID: "PENDINGCERT",
		Validation: &api.TLSValidation{
			Method: "dns-cname-token",
			DNSRecords: []api.TLSValidationDNSRecord{
				{
					Name:  "_dnsauth.example.com.",
					Type:  "CNAME",
					Value: "_expected-token.dcv.digicert.com.",
				},
			},
		},
	})
	if err == nil {
		t.Fatal("ensureValidationRecords() error = nil, want conflict error")
	}
	if !strings.Contains(err.Error(), "refusing to modify existing non-certbro CNAME") {
		t.Fatalf("ensureValidationRecords() error = %q, want foreign conflict error", err.Error())
	}
}

func TestRenewOneTimeoutIncludesPendingResumeHint(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/tls/certificate/PENDINGCERT" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"success": true,
				"response": {
					"id": "PENDINGCERT",
					"status": "pending",
					"common_name": "example.com",
					"product": "RapidSSL",
					"provider": "digicert",
					"dns_names": ["example.com"],
					"order_state": "PENDING",
					"certificate_pem_available": false
				}
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := api.NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	root := t.TempDir()
	outputDir := filepath.Join(root, "example.com")
	if err := deploy.WritePending(outputDir, deploy.PendingMaterial{
		PrivateKeyPEM: []byte("test-key"),
		CSRPEM:        []byte("test-csr"),
		Metadata: deploy.PendingMetadata{
			Action:        "issue",
			CertificateID: "PENDINGCERT",
			CommonName:    "example.com",
			Product:       "RapidSSL",
			RequestedAt:   time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("WritePending() error = %v", err)
	}

	manager := NewManager(client, &config.Store{Version: config.CurrentVersion}, filepath.Join(root, "state.json"))
	_, err = manager.renewOne(context.Background(), &config.ManagedCertificate{
		Name:          "example-com",
		CommonName:    "example.com",
		Product:       "RapidSSL",
		OutputDir:     outputDir,
		CertificateID: "PENDINGCERT",
		PendingAction: "issue",
		ValidityDays:  199,
	}, false, 0, time.Nanosecond, time.Nanosecond)
	if err == nil {
		t.Fatal("renewOne() error = nil, want timeout error")
	}
	if !strings.Contains(err.Error(), "certbro renew --name example-com") {
		t.Fatalf("renewOne() error = %q, want resume hint", err.Error())
	}
}
