# Renewals and Replacement

German version: [../de/renewals-and-replacement.md](../de/renewals-and-replacement.md)

## Normal Renewals

Run all managed renewals:

```sh
sudo certbro --state-file /etc/certbro/state.json renew
```

Run a single managed certificate:

```sh
sudo certbro --state-file /etc/certbro/state.json renew --name example-com
```

When certificate directories are grouped below one root, `certbro` can discover them automatically:

```sh
sudo certbro --state-file /etc/certbro/state.json --certificates-dir /etc/certbro renew
```

If an order is still `pending` when the wait timeout is reached, rerun `certbro renew` later. `certbro` resumes monitoring the existing pending request instead of starting a duplicate order.

## Renewal vs. Reissue

`certbro` chooses the appropriate regfish API flow automatically:

- Renewal order: used for the typical case where a new certificate should be ordered for the existing certificate and remaining lifetime can be credited
- Reissue: used when the contract validity clearly extends beyond the currently issued certificate and the contract supports reissue

In both cases, `certbro` rotates the key material and reuses the managed certificate settings from `certbro.json`.

## Forced Renewal

Force an immediate renewal check:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force
```

By default, `certbro renew` prints progress updates. For quiet automation, add `--quiet`.

## One-Off Validity Override

To request a different lifetime for a forced renewal or fresh renewal order, use `--validity-days`:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force \
  --validity-days 3
```

`--validity-days` applies to renewal orders and fresh new orders in that run. It does not apply to reissues.

To change the stored default for future renewal orders:

```sh
sudo certbro --state-file /etc/certbro/state.json update \
  --name example-com \
  --validity-days 90
```

If a stored `validity_days` exceeds the active CA/B Forum limit at renewal time, `certbro` aborts before placing the order so the lifetime can be adjusted explicitly.

The active CA/B Forum limits are:

- before `2026-03-15`: `398` days maximum
- from `2026-03-15`: `200` days maximum
- from `2027-03-15`: `100` days maximum
- from `2029-03-15`: `47` days maximum

The corresponding `certbro` schedule-aware defaults are `397`, `199`, `99`, and `46` days.

## Replace an Active Certificate Quickly

If you want to replace the currently deployed certificate right away, for example with a different requested lifetime, the usual flow is:

1. Note the current `certificate_id`.
2. Force a renewal with the desired settings.
3. Verify that the new certificate is deployed and active.
4. Revoke the previous certificate in the regfish UI or via the regfish API if required.

Example replacement run:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force \
  --validity-days 3
```

`certbro` does not currently include a dedicated `revoke` command. Revocation of the previous certificate is therefore handled through the regfish Console or the TLS API.
