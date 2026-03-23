// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

package config

import "time"

const secondsPerDay = 24 * 60 * 60

// EffectiveIssuedValidityDays derives the authoritative issued certificate lifetime in days
// from the actual issuance timestamps.
func EffectiveIssuedValidityDays(validFrom, validUntil *time.Time) (int, bool) {
	if validFrom == nil || validUntil == nil {
		return 0, false
	}
	if validUntil.Before(*validFrom) {
		return 0, false
	}
	return int(validUntil.Sub(*validFrom).Seconds()/secondsPerDay + 0.5), true
}

// ConfirmedRenewalBonusDays derives a confirmed renewal bonus from the issued lifetime and the
// purchased base validity. The result is only meaningful after issuance timestamps are known.
func ConfirmedRenewalBonusDays(purchasedValidityDays int, validFrom, validUntil *time.Time) (int, bool) {
	if purchasedValidityDays <= 0 {
		return 0, false
	}
	effectiveValidityDays, ok := EffectiveIssuedValidityDays(validFrom, validUntil)
	if !ok {
		return 0, false
	}
	bonus := effectiveValidityDays - purchasedValidityDays
	if bonus < 0 {
		bonus = 0
	}
	return bonus, true
}
