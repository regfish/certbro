// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const fakePEM = `-----BEGIN CERTIFICATE-----
QQ==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
Qg==
-----END CERTIFICATE-----
`

type server struct {
	mu      sync.Mutex
	apiKey  string
	nextID  int
	records map[string]dnsRecord
	certs   map[string]*certificateState
}

type certificateState struct {
	ID           string
	CommonName   string
	DNSNames     []string
	Product      string
	ValidityDays int
	RenewalOf    string
	Checks       int
	CreatedAt    time.Time
}

type dnsRecord struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	Data       string `json:"data"`
	TTL        int    `json:"ttl,omitempty"`
	Annotation string `json:"annotation,omitempty"`
}

type certificateRequest struct {
	SKU                    string   `json:"sku"`
	CommonName             string   `json:"common_name"`
	DNSNames               []string `json:"dns_names,omitempty"`
	ValidityDays           int      `json:"validity_days,omitempty"`
	RenewalOfCertificateID string   `json:"renewal_of_certificate_id,omitempty"`
}

func main() {
	addr := strings.TrimSpace(os.Getenv("REGFISH_MOCK_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:18081"
	}
	apiKey := strings.TrimSpace(os.Getenv("REGFISH_MOCK_API_KEY"))
	if apiKey == "" {
		apiKey = "smoke-key"
	}

	srv := &server{
		apiKey:  apiKey,
		records: make(map[string]dnsRecord),
		certs:   make(map[string]*certificateState),
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("mock regfish API listening on http://%s", addr)
	if err := http.Serve(listener, http.HandlerFunc(srv.serveHTTP)); err != nil {
		log.Fatal(err)
	}
}

func (s *server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != s.apiKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"message": "Unauthorized",
		})
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/tls/certificate":
		writeJSON(w, http.StatusOK, map[string]any{
			"success":  true,
			"response": []any{},
		})
		return
	case r.Method == http.MethodGet && r.URL.Path == "/tls/products":
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"response": []map[string]any{
				{
					"sku":         "RapidSSL",
					"name":        "RapidSSL",
					"type":        "dv",
					"ca":          "digicert",
					"recommended": true,
				},
				{
					"sku":  "SecureSite",
					"name": "Secure Site",
					"type": "ov",
					"ca":   "digicert",
				},
				{
					"sku":  "SSL123",
					"name": "SSL123",
					"type": "dv",
					"ca":   "digicert",
				},
			},
		})
		return
	case r.Method == http.MethodPost && r.URL.Path == "/tls/certificate":
		s.handleCreateCertificate(w, r)
		return
	case (r.Method == http.MethodPatch || r.Method == http.MethodPost) && r.URL.Path == "/dns/rr":
		s.handleDNSRecord(w, r)
		return
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tls/certificate/") && strings.HasSuffix(r.URL.Path, "/download/pem"):
		s.handleDownloadPEM(w, r)
		return
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tls/certificate/") && strings.HasSuffix(r.URL.Path, "/download/zip"):
		writeJSON(w, http.StatusNotFound, map[string]any{
			"message": "bundle not available",
		})
		return
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tls/certificate/"):
		s.handleGetCertificate(w, r)
		return
	default:
		http.NotFound(w, r)
	}
}

func (s *server) handleCreateCertificate(w http.ResponseWriter, r *http.Request) {
	var req certificateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"message": "invalid JSON",
		})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := fmt.Sprintf("CERT-%03d", s.nextID)
	state := &certificateState{
		ID:           id,
		CommonName:   req.CommonName,
		DNSNames:     req.DNSNames,
		Product:      req.SKU,
		ValidityDays: req.ValidityDays,
		RenewalOf:    req.RenewalOfCertificateID,
		CreatedAt:    time.Now().UTC(),
	}
	if state.ValidityDays == 0 {
		state.ValidityDays = 199
	}
	s.certs[id] = state

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"response": s.certificatePayloadLocked(state, false),
	})
}

func (s *server) handleGetCertificate(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tls/certificate/")
	if idx := strings.IndexRune(id, '/'); idx >= 0 {
		id = id[:idx]
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.certs[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"message": "certificate not found",
		})
		return
	}

	state.Checks++
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"response": s.certificatePayloadLocked(state, state.Checks >= 2),
	})
}

func (s *server) handleDownloadPEM(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tls/certificate/")
	id = strings.TrimSuffix(id, "/download/pem")

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.certs[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"message": "certificate not found",
		})
		return
	}
	if state.Checks < 2 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"message": "certificate not ready",
		})
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	_, _ = w.Write([]byte(fakePEM))
}

func (s *server) handleDNSRecord(w http.ResponseWriter, r *http.Request) {
	var record dnsRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"message": "invalid JSON",
		})
		return
	}

	key := strings.ToLower(strings.TrimSpace(record.Type)) + "|" + strings.ToLower(strings.TrimSpace(record.Name))
	s.mu.Lock()
	s.records[key] = record
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"response": record,
	})
}

func (s *server) certificatePayloadLocked(state *certificateState, issued bool) map[string]any {
	validFrom := state.CreatedAt.Format(time.RFC3339)
	validUntil := state.CreatedAt.Add(time.Duration(state.ValidityDays) * 24 * time.Hour).Format(time.RFC3339)
	response := map[string]any{
		"id":                        state.ID,
		"status":                    "pending",
		"common_name":               state.CommonName,
		"product":                   state.Product,
		"provider":                  "mock-ca",
		"dns_names":                 state.DNSNames,
		"order_state":               "PENDING",
		"reissue_supported":         true,
		"validity_days":             state.ValidityDays,
		"certificate_pem_available": false,
		"validation": map[string]any{
			"method": "dns-cname-token",
			"dns_records": []map[string]any{
				{
					"name":  "_dnsauth." + state.CommonName,
					"type":  "CNAME",
					"value": "token." + state.ID + ".mock.regfish.test",
				},
			},
		},
	}
	if issued {
		response["status"] = "issued"
		response["order_state"] = "ISSUED"
		response["certificate_pem_available"] = true
		response["valid_from"] = validFrom
		response["valid_until"] = validUntil
		response["contract_valid_from"] = validFrom
		response["contract_valid_until"] = validUntil
	}
	return response
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
