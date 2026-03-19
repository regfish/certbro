// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package certcrypto

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"testing"
)

func TestNormalizeDNSNames(t *testing.T) {
	got := NormalizeDNSNames("WWW.Example.com.", []string{"api.example.com", "www.example.com", "API.example.com."})
	want := []string{"www.example.com", "api.example.com"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeDNSNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitFullChainPEM(t *testing.T) {
	fullChain := []byte(`-----BEGIN CERTIFICATE-----
QQ==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
Qg==
-----END CERTIFICATE-----
`)

	certPEM, chainPEM, err := SplitFullChainPEM(fullChain)
	if err != nil {
		t.Fatalf("SplitFullChainPEM() error = %v", err)
	}
	if bytes.Equal(certPEM, chainPEM) {
		t.Fatalf("expected leaf and chain to differ")
	}
	if !bytes.Contains(certPEM, []byte("QQ==")) {
		t.Fatalf("leaf certificate missing first block")
	}
	if !bytes.Contains(chainPEM, []byte("Qg==")) {
		t.Fatalf("chain certificate missing second block")
	}
}

func TestGenerateRSAKeyAndCSR(t *testing.T) {
	material, err := GenerateKeyAndCSR("example.com", []string{"www.example.com"}, KeyOptions{
		Type:    KeyTypeRSA,
		RSABits: 3072,
	})
	if err != nil {
		t.Fatalf("GenerateKeyAndCSR() error = %v", err)
	}

	block, _ := pem.Decode(material.PrivateKeyPEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatalf("unexpected private key PEM block: %#v", block)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKCS1PrivateKey() error = %v", err)
	}
	if key.N.BitLen() != 3072 {
		t.Fatalf("RSA bit length = %d, want 3072", key.N.BitLen())
	}
}

func TestGenerateECDSAKeyAndCSR(t *testing.T) {
	material, err := GenerateKeyAndCSR("example.com", []string{"www.example.com"}, KeyOptions{
		Type:       KeyTypeECDSA,
		ECDSACurve: "p384",
	})
	if err != nil {
		t.Fatalf("GenerateKeyAndCSR() error = %v", err)
	}

	block, _ := pem.Decode(material.PrivateKeyPEM)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		t.Fatalf("unexpected private key PEM block: %#v", block)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParseECPrivateKey() error = %v", err)
	}
	if got := key.Params().BitSize; got != 384 {
		t.Fatalf("ECDSA curve size = %d, want 384", got)
	}
}
