// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package certcrypto contains key and CSR helpers used by certbro.
package certcrypto

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"strings"
)

const (
	// KeyTypeRSA selects RSA private keys.
	KeyTypeRSA = "rsa"
	// KeyTypeECDSA selects ECDSA private keys.
	KeyTypeECDSA = "ecdsa"
)

// KeyOptions controls local key generation for CSRs and renewals.
type KeyOptions struct {
	Type       string
	RSABits    int
	ECDSACurve string
}

// GeneratedMaterial contains a private key and CSR pair in PEM encoding.
type GeneratedMaterial struct {
	PrivateKeyPEM []byte
	CSRPEM        []byte
}

// GenerateKeyAndCSR creates a private key and CSR for the given certificate subject names.
func GenerateKeyAndCSR(commonName string, dnsNames []string, opts KeyOptions) (*GeneratedMaterial, error) {
	keyType := strings.ToLower(strings.TrimSpace(opts.Type))
	if keyType == "" {
		keyType = KeyTypeRSA
	}

	switch keyType {
	case KeyTypeRSA:
		return generateRSAKeyAndCSR(commonName, dnsNames, opts.RSABits)
	case KeyTypeECDSA:
		return generateECDSAKeyAndCSR(commonName, dnsNames, opts.ECDSACurve)
	default:
		return nil, fmt.Errorf("unsupported key type %q", opts.Type)
	}
}

func generateRSAKeyAndCSR(commonName string, dnsNames []string, bits int) (*GeneratedMaterial, error) {
	if bits == 0 {
		bits = 2048
	}
	if bits < 2048 {
		return nil, fmt.Errorf("RSA key size must be at least 2048 bits")
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	return generateCSR(commonName, dnsNames, privateKey, pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}))
}

func generateECDSAKeyAndCSR(commonName string, dnsNames []string, curveName string) (*GeneratedMaterial, error) {
	curve, normalizedCurve, err := ellipticCurve(curveName)
	if err != nil {
		return nil, err
	}

	privateKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ECDSA key on %s: %w", normalizedCurve, err)
	}

	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal ECDSA private key: %w", err)
	}

	return generateCSR(commonName, dnsNames, privateKey, pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	}))
}

func generateCSR(commonName string, dnsNames []string, privateKey any, privateKeyPEM []byte) (*GeneratedMaterial, error) {
	allNames := NormalizeDNSNames(commonName, dnsNames)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: strings.TrimSpace(commonName),
		},
		DNSNames: allNames,
	}, privateKey)
	if err != nil {
		return nil, fmt.Errorf("create CSR: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrDER,
	})

	return &GeneratedMaterial{
		PrivateKeyPEM: privateKeyPEM,
		CSRPEM:        csrPEM,
	}, nil
}

func ellipticCurve(curveName string) (elliptic.Curve, string, error) {
	switch strings.ToLower(strings.TrimSpace(curveName)) {
	case "", "p256", "prime256v1", "secp256r1":
		return elliptic.P256(), "p256", nil
	case "p384", "secp384r1":
		return elliptic.P384(), "p384", nil
	case "p521", "secp521r1":
		return elliptic.P521(), "p521", nil
	default:
		return nil, "", fmt.Errorf("unsupported ECDSA curve %q", curveName)
	}
}

// NormalizeDNSNames lower-cases, deduplicates, and normalizes common name and SAN input.
func NormalizeDNSNames(commonName string, dnsNames []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(dnsNames)+1)

	add := func(name string) {
		name = strings.TrimSpace(strings.TrimSuffix(name, "."))
		if name == "" {
			return
		}
		name = strings.ToLower(name)
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}

	add(commonName)
	for _, name := range dnsNames {
		add(name)
	}

	return out
}

// SplitFullChainPEM splits a concatenated fullchain PEM into leaf and issuer chain sections.
func SplitFullChainPEM(fullChain []byte) (certPEM []byte, chainPEM []byte, err error) {
	remaining := bytes.TrimSpace(fullChain)
	if len(remaining) == 0 {
		return nil, nil, fmt.Errorf("empty PEM payload")
	}

	var certBlocks [][]byte
	for len(remaining) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			return nil, nil, fmt.Errorf("failed to decode PEM block")
		}
		if block.Type == "CERTIFICATE" {
			certBlocks = append(certBlocks, pem.EncodeToMemory(block))
		}
		remaining = bytes.TrimSpace(rest)
	}

	if len(certBlocks) == 0 {
		return nil, nil, fmt.Errorf("no CERTIFICATE blocks found in PEM payload")
	}

	certPEM = certBlocks[0]
	if len(certBlocks) > 1 {
		chainPEM = bytes.Join(certBlocks[1:], nil)
	}
	return certPEM, chainPEM, nil
}
