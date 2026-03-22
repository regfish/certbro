# Changes

This file summarizes notable changes from the most recent committed updates.

## 2026-03-22

- Added English and German validity-management guides and updated the existing renewal, issuing, quick-start, and Ubuntu example docs to describe the new lifetime rules and short-lifetime examples correctly.
- Expanded regression coverage for schedule transitions, renewal timing validation, legacy lead-time normalization, and recent-issuance cooldown behavior.
- Added a 48-hour cooldown after fresh issuance. Non-forced renewal runs now skip certificates that were issued less than 48 hours ago.
- Added self-healing for legacy managed certificates with overly large lead times. During renewals, stored lead times are reduced to `validity_days - 1` when needed.
- Added renewal timing validation to prevent immediate renewal or reissue loops. New and updated configurations now require `validity_days` to be greater than both `renew_before_days` and `reissue_lead_days`.
- Added automatic normalization of stored `validity_days` during renewals. If an older managed certificate still stores a now-too-large value, `certbro renew` uses the current schedule-aware default and persists the adjusted value after a successful run.
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
