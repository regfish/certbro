// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package api provides a small regfish API client used by certbro.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the default regfish API endpoint used by certbro.
const DefaultBaseURL = "https://api.regfish.com"

// Client wraps the regfish API base URL, authentication, and HTTP transport.
type Client struct {
	BaseURL    string
	APIKey     string
	UserAgent  string
	HTTPClient *http.Client
}

// UserAgentOptions describes the metadata segments appended to the User-Agent.
type UserAgentOptions struct {
	Product      string
	Version      string
	GOOS         string
	GOARCH       string
	ContactEmail string
	Instance     string
}

type apiEnvelope[T any] struct {
	Success  bool   `json:"success"`
	Code     int    `json:"code"`
	Response T      `json:"response"`
	Message  string `json:"message"`
	Error    string `json:"error"`
}

// APIError represents a non-success HTTP response from the regfish API.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
	Detail     string
	Body       string
}

// Error returns a compact error string for logging and CLI output.
func (e *APIError) Error() string {
	parts := []string{fmt.Sprintf("%s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Detail != "" && e.Detail != e.Message {
		parts = append(parts, e.Detail)
	}
	return strings.Join(parts, ": ")
}

// IsStatus reports whether err is an APIError with the given HTTP status code.
func IsStatus(err error, statusCode int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == statusCode
}

// DNSRecord is the regfish DNS record payload used for create and patch calls.
type DNSRecord struct {
	ID         int    `json:"id,omitempty"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Data       string `json:"data"`
	TTL        int    `json:"ttl,omitempty"`
	Annotation string `json:"annotation,omitempty"`
	Auto       bool   `json:"auto,omitempty"`
	Active     bool   `json:"active,omitempty"`
}

// TLSValidationDNSRecord describes one DNS validation record returned by the TLS API.
type TLSValidationDNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// TLSValidation contains DCV instructions for certificate issuance or reissue.
type TLSValidation struct {
	Method       string                   `json:"method"`
	DNSRecords   []TLSValidationDNSRecord `json:"dns_records,omitempty"`
	EmailTargets []string                 `json:"email_targets,omitempty"`
}

// TLSCertificateReissue holds the current reissue state for a certificate order.
type TLSCertificateReissue struct {
	ID            int            `json:"id"`
	Status        string         `json:"status"`
	CommonName    string         `json:"common_name"`
	DNSNames      []string       `json:"dns_names"`
	OrderState    string         `json:"order_state,omitempty"`
	RequestedAt   *time.Time     `json:"requested_at,omitempty"`
	LastUpdatedAt *time.Time     `json:"last_updated_at,omitempty"`
	Validation    *TLSValidation `json:"validation,omitempty"`
}

// TLSCertificate models the public TLS certificate resource returned by the API.
type TLSCertificate struct {
	ID                     string                 `json:"id"`
	Status                 string                 `json:"status"`
	CommonName             string                 `json:"common_name"`
	Product                string                 `json:"product"`
	Provider               string                 `json:"provider"`
	DNSNames               []string               `json:"dns_names"`
	OrderState             string                 `json:"order_state,omitempty"`
	RevocationScope        string                 `json:"revocation_scope,omitempty"`
	RevocationPendingScope string                 `json:"revocation_pending_scope,omitempty"`
	RenewalSupported       bool                   `json:"renewal_supported,omitempty"`
	ReissueSupported       bool                   `json:"reissue_supported"`
	ValidityDays           int                    `json:"validity_days,omitempty"`
	SerialNumber           string                 `json:"serial_number,omitempty"`
	ValidFrom              *time.Time             `json:"valid_from,omitempty"`
	ValidUntil             *time.Time             `json:"valid_until,omitempty"`
	ContractValidFrom      *time.Time             `json:"contract_valid_from,omitempty"`
	ContractValidUntil     *time.Time             `json:"contract_valid_until,omitempty"`
	LastStatusCheck        *time.Time             `json:"last_status_check,omitempty"`
	CertificateAvailable   bool                   `json:"certificate_pem_available"`
	OrderCancellable       bool                   `json:"order_cancellable,omitempty"`
	OrderCancellationMode  string                 `json:"order_cancellation_mode,omitempty"`
	OrderCancellableUntil  *time.Time             `json:"order_cancellable_until,omitempty"`
	Validation             *TLSValidation         `json:"validation,omitempty"`
	Reissue                *TLSCertificateReissue `json:"reissue,omitempty"`
}

// TLSProduct describes one entry from the TLS product catalog.
type TLSProduct struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	CA          string `json:"ca"`
	Recommended bool   `json:"recommended,omitempty"`
}

// TLSCertificateRequest is the order or renewal payload for POST /tls/certificate.
type TLSCertificateRequest struct {
	SKU                    string   `json:"sku"`
	CommonName             string   `json:"common_name"`
	DNSNames               []string `json:"dns_names,omitempty"`
	CSR                    string   `json:"csr"`
	DCVMethod              string   `json:"dcv_method"`
	DCVEmails              []string `json:"dcv_emails,omitempty"`
	Organization           int      `json:"org_id,omitempty"`
	RenewalOfCertificateID string   `json:"renewal_of_certificate_id,omitempty"`
	ValidityDays           int      `json:"validity_days,omitempty"`
}

// TLSCertificateReissueRequest is the payload for POST /tls/certificate/{id}/reissue.
type TLSCertificateReissueRequest struct {
	CSR        string   `json:"csr"`
	CommonName string   `json:"common_name,omitempty"`
	DNSNames   []string `json:"dns_names,omitempty"`
	DCVMethod  string   `json:"dcv_method"`
	Comments   string   `json:"comments,omitempty"`
}

// NewClient constructs a regfish API client with the given credentials and base URL.
func NewClient(apiKey, baseURL, userAgent string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("missing API key")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		APIKey:    apiKey,
		UserAgent: userAgent,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// BuildUserAgent assembles the certbro User-Agent header from static and runtime metadata.
func BuildUserAgent(opts UserAgentOptions) string {
	product := sanitizeUserAgentToken(opts.Product)
	if product == "" {
		product = "certbro"
	}
	version := sanitizeUserAgentToken(opts.Version)
	if version == "" {
		version = "dev"
	}

	var details []string
	if goos := sanitizeUserAgentValue(opts.GOOS); goos != "" {
		details = append(details, "os="+goos)
	}
	if goarch := sanitizeUserAgentValue(opts.GOARCH); goarch != "" {
		details = append(details, "arch="+goarch)
	}
	if instance := sanitizeUserAgentValue(opts.Instance); instance != "" {
		details = append(details, "instance="+instance)
	}
	if contactEmail := sanitizeUserAgentValue(opts.ContactEmail); contactEmail != "" {
		details = append(details, "contact="+contactEmail)
	}

	userAgent := product + "/" + version
	if len(details) > 0 {
		userAgent += " (" + strings.Join(details, "; ") + ")"
	}
	return userAgent
}

// ListCertificates retrieves all TLS certificates visible to the authenticated customer.
func (c *Client) ListCertificates(ctx context.Context) ([]TLSCertificate, error) {
	return requestJSON[[]TLSCertificate](ctx, c, http.MethodGet, "/tls/certificate", nil)
}

// ListTLSProducts retrieves the currently available TLS product catalog.
func (c *Client) ListTLSProducts(ctx context.Context) ([]TLSProduct, error) {
	return requestJSON[[]TLSProduct](ctx, c, http.MethodGet, "/tls/products", nil)
}

// ValidateCredentials performs a lightweight authenticated request to verify API access.
func (c *Client) ValidateCredentials(ctx context.Context) error {
	_, _, err := c.do(ctx, http.MethodGet, "/tls/certificate", nil, "application/json")
	return err
}

// GetCertificate retrieves a single TLS certificate by its public certificate ID.
func (c *Client) GetCertificate(ctx context.Context, certificateID string) (*TLSCertificate, error) {
	cert, err := requestJSON[TLSCertificate](ctx, c, http.MethodGet, "/tls/certificate/"+certificateID, nil)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// CreateCertificate submits a new certificate order or renewal order.
func (c *Client) CreateCertificate(ctx context.Context, req TLSCertificateRequest) (*TLSCertificate, error) {
	cert, err := requestJSON[TLSCertificate](ctx, c, http.MethodPost, "/tls/certificate", req)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// ReissueCertificate submits a reissue request for an existing TLS certificate order.
func (c *Client) ReissueCertificate(ctx context.Context, certificateID string, req TLSCertificateReissueRequest) (*TLSCertificate, error) {
	cert, err := requestJSON[TLSCertificate](ctx, c, http.MethodPost, "/tls/certificate/"+certificateID+"/reissue", req)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// DownloadCertificate downloads the certificate bundle in the requested format.
func (c *Client) DownloadCertificate(ctx context.Context, certificateID, format string) ([]byte, error) {
	_, body, err := c.do(ctx, http.MethodGet, "/tls/certificate/"+certificateID+"/download/"+format, nil, "")
	if err != nil {
		return nil, err
	}
	return body, nil
}

// CreateDNSRecord creates a DNS record through the regfish DNS API.
func (c *Client) CreateDNSRecord(ctx context.Context, record DNSRecord) (*DNSRecord, error) {
	out, err := requestJSON[DNSRecord](ctx, c, http.MethodPost, "/dns/rr", record)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDNSRecords lists all DNS records for one managed zone.
func (c *Client) ListDNSRecords(ctx context.Context, domain string) ([]DNSRecord, error) {
	return requestJSON[[]DNSRecord](ctx, c, http.MethodGet, "/dns/"+strings.TrimSpace(domain)+"/rr", nil)
}

// PatchDNSRecord updates a DNS record through the regfish DNS API.
func (c *Client) PatchDNSRecord(ctx context.Context, record DNSRecord) (*DNSRecord, error) {
	out, err := requestJSON[DNSRecord](ctx, c, http.MethodPatch, "/dns/rr", record)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpsertDNSRecord updates a DNS record and falls back to create when it does not yet exist.
func (c *Client) UpsertDNSRecord(ctx context.Context, record DNSRecord) (*DNSRecord, error) {
	out, err := c.PatchDNSRecord(ctx, record)
	if err == nil {
		return out, nil
	}
	if !IsStatus(err, http.StatusNotFound) {
		return nil, err
	}
	return c.CreateDNSRecord(ctx, record)
}

// DeleteDNSRecord removes a DNS record by rrid.
func (c *Client) DeleteDNSRecord(ctx context.Context, rrid int) error {
	_, _, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/dns/rr/%d", rrid), nil, "application/json")
	return err
}

func requestJSON[T any](ctx context.Context, client *Client, method, path string, body any) (T, error) {
	var zero T
	_, raw, err := client.do(ctx, method, path, body, "application/json")
	if err != nil {
		return zero, err
	}

	var envelope apiEnvelope[T]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return zero, fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	if !envelope.Success {
		return zero, &APIError{
			Method:     method,
			Path:       path,
			StatusCode: http.StatusBadGateway,
			Message:    envelope.Message,
			Detail:     envelope.Error,
			Body:       string(raw),
		}
	}
	return envelope.Response, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, accept string) (*http.Response, []byte, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal %s %s request: %w", method, path, err)
		}
		payload = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, payload)
	if err != nil {
		return nil, nil, fmt.Errorf("build %s %s request: %w", method, path, err)
	}

	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("User-Agent", c.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s request failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s %s response: %w", method, path, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, raw, nil
	}

	var apiMsg struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(raw, &apiMsg)

	return nil, nil, &APIError{
		Method:     method,
		Path:       path,
		StatusCode: resp.StatusCode,
		Message:    apiMsg.Message,
		Detail:     apiMsg.Error,
		Body:       string(raw),
	}
}

func sanitizeUserAgentToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sanitizeUserAgentValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("@._:+/-", r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
