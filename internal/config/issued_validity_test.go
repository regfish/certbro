// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"
	"time"
)

func TestEffectiveIssuedValidityDays(t *testing.T) {
	validFrom := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	validUntil := validFrom.Add(199 * 24 * time.Hour)

	got, ok := EffectiveIssuedValidityDays(&validFrom, &validUntil)
	if !ok {
		t.Fatal("EffectiveIssuedValidityDays() ok = false, want true")
	}
	if got != 199 {
		t.Fatalf("EffectiveIssuedValidityDays() = %d, want 199", got)
	}
}

func TestConfirmedRenewalBonusDays(t *testing.T) {
	validFrom := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	validUntil := validFrom.Add(206 * 24 * time.Hour)

	effective, ok := EffectiveIssuedValidityDays(&validFrom, &validUntil)
	if !ok {
		t.Fatal("EffectiveIssuedValidityDays() ok = false, want true")
	}
	if effective != 206 {
		t.Fatalf("EffectiveIssuedValidityDays() = %d, want 206", effective)
	}

	bonus, ok := ConfirmedRenewalBonusDays(199, &validFrom, &validUntil)
	if !ok {
		t.Fatal("ConfirmedRenewalBonusDays() ok = false, want true")
	}
	if bonus != 7 {
		t.Fatalf("ConfirmedRenewalBonusDays() = %d, want 7", bonus)
	}
}
