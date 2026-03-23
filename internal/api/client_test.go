// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/regfish/certbro/internal/testutil"
)

func TestBuildUserAgent(t *testing.T) {
	got := BuildUserAgent(UserAgentOptions{
		Product:      "certbro",
		Version:      "v0.1.0",
		GOOS:         "linux",
		GOARCH:       "amd64",
		ContactEmail: "ops@example.com",
		Instance:     "web-01",
	})

	want := "certbro/v0.1.0 (os=linux; arch=amd64; instance=web-01; contact=ops@example.com)"
	if got != want {
		t.Fatalf("BuildUserAgent() = %q, want %q", got, want)
	}
}

func TestListCertificatesSendsUserAgentHeader(t *testing.T) {
	wantUA := "certbro/v0.1.0 (os=linux; arch=amd64; contact=ops@example.com)"
	var gotUA string
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"response":[]}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := NewClient("secret", server.URL, wantUA)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	if _, err := client.ListCertificates(context.Background()); err != nil {
		t.Fatalf("ListCertificates() error = %v", err)
	}
	if gotUA != wantUA {
		t.Fatalf("User-Agent = %q, want %q", gotUA, wantUA)
	}
}

func TestListTLSProductsParsesCatalog(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/tls/products" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"response": [
				{
					"sku": "RapidSSL",
					"name": "RapidSSL",
					"type": "dv",
					"ca": "digicert",
					"recommended": true,
					"validation_level": "dv",
					"organization_required": false
				},
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
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	products, err := client.ListTLSProducts(context.Background())
	if err != nil {
		t.Fatalf("ListTLSProducts() error = %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("len(products) = %d, want 2", len(products))
	}
	if products[0].SKU != "RapidSSL" || !products[0].Recommended {
		t.Fatalf("products[0] = %#v", products[0])
	}
	if products[0].ValidationLevel != "dv" || products[0].OrganizationRequired {
		t.Fatalf("products[0] validation fields = %#v", products[0])
	}
	if products[1].SKU != "SecureSite" {
		t.Fatalf("products[1] = %#v", products[1])
	}
	if products[1].ValidationLevel != "ov" || !products[1].OrganizationRequired {
		t.Fatalf("products[1] validation fields = %#v", products[1])
	}
}

func TestGetCertificateParsesCurrentOpenAPIFields(t *testing.T) {
	server, err := testutil.NewLocalServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"response": {
				"id": "7K9QW3M2ZT8HJ",
				"status": "issued",
				"common_name": "example.com",
				"product": "RapidSSL",
				"provider": "digicert",
				"dns_names": ["example.com", "www.example.com"],
				"order_state": "ISSUED",
				"action_required": false,
				"pending_reason": "validation_pending",
				"pending_message": "The TLS certificate order is waiting for domain validation.",
				"completion_url": "https://dash.regfish.com/my/certs/7K9QW3M2ZT8HJ/complete",
				"organization_id": 42,
				"revocation_scope": "certificate",
				"revocation_pending_scope": "order",
				"renewal_supported": true,
					"reissue_supported": true,
					"validity_days": 199,
					"renewal_bonus_days": 7,
					"valid_from": "2026-03-18T10:00:00Z",
					"valid_until": "2026-10-10T10:00:00Z",
					"contract_valid_from": "2026-03-18T10:00:00Z",
					"contract_valid_until": "2026-10-10T10:00:00Z",
					"last_status_check": "2026-03-18T10:05:00Z",
				"certificate_pem_available": true,
				"order_cancellable": true,
				"order_cancellation_mode": "revoke_issued",
				"order_cancellable_until": "2026-03-20T10:00:00Z",
				"organization": {
					"id": 42,
					"name": "Example GmbH",
					"status": "validated",
					"usable_for_ordering": true
				}
			}
		}`))
	}))
	if err != nil {
		t.Fatalf("NewLocalServer() error = %v", err)
	}
	defer server.Close()

	client, err := NewClient("secret", server.URL, "certbro/test")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.HTTPClient = server.Client()

	cert, err := client.GetCertificate(context.Background(), "7K9QW3M2ZT8HJ")
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	if cert.ID != "7K9QW3M2ZT8HJ" {
		t.Fatalf("cert.ID = %q", cert.ID)
	}
	if cert.RevocationScope != "certificate" || cert.RevocationPendingScope != "order" {
		t.Fatalf("revocation scopes = %q / %q", cert.RevocationScope, cert.RevocationPendingScope)
	}
	if cert.ActionRequired {
		t.Fatalf("ActionRequired = %v, want false", cert.ActionRequired)
	}
	if cert.PendingReason != "validation_pending" || cert.OrganizationID != 42 {
		t.Fatalf("pending/org fields = %#v", cert)
	}
	if cert.CompletionURL != "https://dash.regfish.com/my/certs/7K9QW3M2ZT8HJ/complete" {
		t.Fatalf("CompletionURL = %q", cert.CompletionURL)
	}
	if cert.Organization == nil || cert.Organization.Name != "Example GmbH" || !cert.Organization.UsableForOrdering {
		t.Fatalf("Organization = %#v", cert.Organization)
	}
	if !cert.RenewalSupported {
		t.Fatalf("RenewalSupported = %v, want true", cert.RenewalSupported)
	}
	if cert.RenewalBonusDays == nil || *cert.RenewalBonusDays != 7 {
		t.Fatalf("RenewalBonusDays = %v, want 7", cert.RenewalBonusDays)
	}
	if !cert.OrderCancellable || cert.OrderCancellationMode != "revoke_issued" {
		t.Fatalf("order cancellation fields = %#v", cert)
	}
	wantUntil := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	if cert.OrderCancellableUntil == nil || !cert.OrderCancellableUntil.Equal(wantUntil) {
		t.Fatalf("OrderCancellableUntil = %v, want %v", cert.OrderCancellableUntil, wantUntil)
	}
}
