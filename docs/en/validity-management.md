# Validity Management

German version: [../de/validity-management.md](../de/validity-management.md)

## Overview

`certbro` stores the purchased base lifetime per managed certificate and reuses it for future renewal orders and fresh new orders.

For `example.com`, the stored lifetime can be changed at any time. `certbro` then uses the new value as the purchased base validity for later renewals as long as the value remains within the active schedule-aware limit.

## Change the Stored Lifetime Manually

Example: `example.com` was initially ordered with `3` days and should use `30` days for future renewals.

Update the stored setting:

```sh
sudo certbro update --name example-com --validity-days 30
```

Then run the next renewal as usual:

```sh
sudo certbro renew --name example-com
```

If you want to replace the current certificate immediately with the new purchased base lifetime:

```sh
sudo certbro renew --name example-com --force --validity-days 30
```

For very short-lived certificates, keep the lead times below the purchased base lifetime. Example for a `3` day certificate:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --validity-days 3 \
  --renew-before-days 2 \
  --reissue-lead-days 2
```

## Automatic Lifetime Adjustment

`certbro` follows the CA/B Forum validity schedule, but with a one-day safety margin. That means `certbro` starts using the upcoming lower limit one day before the official transition date.

Official CA/B Forum maximum lifetimes:

- from `2026-03-15`: `200` days
- from `2027-03-15`: `100` days
- from `2029-03-15`: `47` days

`certbro` schedule-aware defaults:

- from `2026-03-14`: `199` days
- from `2027-03-14`: `99` days
- from `2029-03-14`: `46` days

## What Happens to Older Stored Values

If a managed certificate still stores a now-too-large value, `certbro renew` adjusts it automatically before ordering.

Example:

- `example.com` was previously stored with `199` days
- the renewal happens on or after `2027-03-14`
- the next effective order uses `99` days
- the stored `validity_days` is updated during renewal processing as the managed state is refreshed

This auto-adjustment applies to stored values during renewal processing. Explicit CLI input remains strict:

- `certbro issue --validity-days ...` is validated immediately
- `certbro update --validity-days ...` is validated immediately
- `certbro renew --validity-days ...` is validated immediately
- the same timing rules also apply to `certbro issue-pair` and `certbro import`

So `certbro` keeps existing managed certificates operational across future schedule changes, while still rejecting explicitly invalid new inputs.
