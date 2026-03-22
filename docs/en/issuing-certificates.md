# Issuing Certificates

German version: [../de/issuing-certificates.md](../de/issuing-certificates.md)

## Basic Issue

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

`certbro issue` creates a fresh private key and CSR locally, places the order through the regfish TLS API, provisions the required `dns-cname-token` validation records through the regfish DNS API, waits for issuance, downloads the certificate, and deploys it to stable paths under `live/`.

If `--validity-days` is omitted, `certbro` uses a date-aware default aligned with the current CA/B Forum validity schedule.

## Validity Schedule

`certbro` follows the CA/B Forum schedule for publicly trusted TLS certificate lifetimes and starts using the upcoming lower default one day earlier as a safety margin.

- Certificates issued before `2026-03-14`: maximum `398` days, default `397`
- Certificates issued on or after `2026-03-14` and before `2027-03-14`: maximum `200` days, default `199`
- Certificates issued on or after `2027-03-14` and before `2029-03-14`: maximum `100` days, default `99`
- Certificates issued on or after `2029-03-14`: maximum `47` days, default `46`

If you pass `--validity-days`, `certbro` validates the requested value against the active limit before placing the order.

The requested lifetime must also be greater than both `--renew-before-days` and `--reissue-lead-days`. This prevents immediate follow-up renewals or reissues right after issuance.

## Subject Alternative Names

Repeat `--dns-name` for each SAN:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --dns-name api.example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

The SAN list is kept in the local management state and reused for later renewals and reissues.

## Product Selection

The requested `--product` is validated against the live regfish TLS product catalog before an order is created.

Example:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

If the product does not exist, `certbro` aborts and prints the available product identifiers returned by the API.

## Key Algorithms

RSA example:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --key-type rsa \
  --rsa-bits 3072 \
  --output-dir /etc/certbro/example.com
```

ECDSA example:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --key-type ecdsa \
  --ecdsa-curve p384 \
  --output-dir /etc/certbro/example.com
```

`certbro` rotates key material on every fresh issue, renewal order, and reissue.

If you want to operate RSA and ECDSA variants in parallel for the same hostname, use [`issue-pair`](dual-certificates.md).

## Progress Output

By default, `certbro issue` prints progress updates for key phases such as product validation, DCV provisioning, waiting for issuance, and deployment.

For quiet automation or scripted usage, add `--quiet`:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com \
  --quiet
```
