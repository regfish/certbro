// Copyright 2026 regfish GmbH
// SPDX-License-Identifier: Apache-2.0

// Package tlsmeta contains shared TLS metadata helpers used across API, state, and deployment code.
package tlsmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// OrganizationID is the public TLS organization id used by the regfish TLS API.
//
// The current OpenAPI contract uses string ids such as `hdl_ABCDEFGHJKLMN`. For
// backwards compatibility with older local state files, JSON unmarshalling also
// accepts legacy numeric ids and stores them as strings.
type OrganizationID string

// NormalizeOrganizationID trims surrounding whitespace from a public organization id.
func NormalizeOrganizationID(value string) OrganizationID {
	return OrganizationID(strings.TrimSpace(value))
}

// String returns the normalized textual form of the organization id.
func (id OrganizationID) String() string {
	return strings.TrimSpace(string(id))
}

// IsZero reports whether the organization id is empty after trimming.
func (id OrganizationID) IsZero() bool {
	return id.String() == ""
}

// MarshalJSON serializes the organization id as a normalized JSON string.
func (id OrganizationID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON accepts the current string form and legacy numeric ids.
func (id *OrganizationID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return fmt.Errorf("tls organization id target is nil")
	}

	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		*id = NormalizeOrganizationID(value)
		return nil
	}

	var legacy json.Number
	if err := json.Unmarshal(data, &legacy); err == nil {
		*id = NormalizeOrganizationID(legacy.String())
		return nil
	}

	return fmt.Errorf("decode tls organization id: expected string or number, got %s", string(data))
}
