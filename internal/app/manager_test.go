// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"encoding/json"
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

	result, err := manager.Import(context.Background(), config.ManagedCertificate{
		Name:          "example-com",
		OutputDir:     outputDir,
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
