# Changes

This file summarizes notable changes from the most recent committed updates.

## 2026-03-23

- `certbro issue`, `certbro import`, and `certbro issue-pair` now derive their output directories from `--certificates-dir` plus `common-name` when no explicit output path is passed.
- Removed redundant `--output-dir` and `--output-dir-base` flags from the basic English and German examples, walkthroughs, and quick-start guides.
- Simplified the README, getting-started guides, workflow overview, renewal/import/automation guides, and Ubuntu walkthroughs so the first example commands now rely on the Linux defaults instead of repeating default flags.
- Removed default-valued parameters such as `/etc/certbro` state and certificates paths, the default DV product, and default key or timer settings from basic example commands. Non-default flags remain documented where they actually change behavior.
- Restructured the larger Ubuntu walkthroughs so the initial `issue`, `renew`, `install`, and `list` examples stay flat and copy-pasteable, while non-default product and lifetime overrides are now described as explicit optional follow-up variants.

---

- Aligned the OV/business docs and example walkthroughs with the current TLS API semantics. The guides now describe `certbro issue` as blocking by default for DV products only.
- Documented the `--org-id` path for OV/business orders, so existing usable organizations can be pre-linked before ordering.
- Updated staged OV/business completion fixtures and tests to the current Console completion route under `/my/certs/{certificate_id}/complete`.

---

- Added staged OV/business order support for the TLS API completion flow.
- `certbro issue` now treats `action_required=true` as a successful staged start, stores pending material locally, prints the Console `completion_url`, and exits without blocking for issuance.
- `certbro renew` now resumes staged OV/business orders, keeps action-required orders pending without duplicating them, provisions DCV once validation records appear, and finalizes download and deployment after issuance.
- Extended pending metadata, list output, and regression coverage for staged OV/business orders, including `action_required`, `pending_reason`, `pending_message`, `completion_url`, and `organization_id`.

---

- Aligned renewal handling with the updated TLS API semantics for provider-linked renewals.
- `validity_days` is now treated consistently as the purchased base lifetime, while the effective issued lifetime is derived from `valid_from` and `valid_until`.
- Added issued-lifetime helpers and surfaced `purchased_validity_days`, `effective_validity_days`, and `renewal_bonus_days` in CLI output and deployment metadata.
- Updated docs and tests so `certbro` no longer implies guaranteed renewal credit before issuance.

---

## 2026-03-22

- Added English and German validity-management guides and updated the existing renewal, issuing, quick-start, and Ubuntu example docs to describe the new lifetime rules and short-lifetime examples correctly.
- Expanded regression coverage for schedule transitions, renewal timing validation, legacy lead-time normalization, and recent-issuance cooldown behavior.
- Added a 48-hour cooldown after fresh issuance. Non-forced renewal runs now skip certificates that were issued less than 48 hours ago.
- Added self-healing for legacy managed certificates with overly large lead times. During renewals, stored lead times are reduced to `validity_days - 1` when needed.
- Added renewal timing validation to prevent immediate renewal or reissue loops. New and updated configurations now require `validity_days` to be greater than both `renew_before_days` and `reissue_lead_days`.
- Added automatic normalization of stored `validity_days` during renewals. If an older managed certificate still stores a now-too-large value, `certbro renew` uses the current schedule-aware default and persists the adjusted value during renewal processing.
- Moved the schedule-aware CA/B validity handling one day earlier than the official transition dates, so `certbro` starts using the upcoming lower defaults on `2026-03-14`, `2027-03-14`, and `2029-03-14`.

---

- The Linux install check now passes an explicit certificates root to `certbro install`, keeping the smoke run self-contained and repeatable.
- The smoke test now uses temporary values for `CERTBRO_CERTIFICATES_DIR` and `CERTBRO_RENEW_LOCK_FILE`, so it no longer touches `/etc/certbro`.
- Hardened `scripts/test-smoke.sh` for the Linux default paths introduced in the previous commit.

---

- Cleaned up repository maintenance scripts and ignored local helper script copies in `.gitignore`.
- Updated the English and German README/getting-started guides to document the Linux defaults and the system-wide deployment layout.
- Added regression tests for the Linux default paths and for preserving verified API settings during renew discovery.
- Fixed `renew` discovery so runs with a certificates directory preserve verified global API configuration, including `api_key_validated_at`.
- Updated the CLI root defaults so `--state-file` and `--certificates-dir` follow those Linux system paths by default.
- Switched the default Linux paths to `/etc/certbro/state.json` for the global state file and `/etc/certbro` for managed certificate directories.
