# Issuing Certificates

German version: [../de/issuing-certificates.md](../de/issuing-certificates.md)

## Basic Issue

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com
```

If `--output-dir` is omitted, `certbro` writes to `<certificates-dir>/<common-name>`, which is `/etc/certbro/example.com` with the Linux defaults.

For DV products, `certbro issue` creates a fresh private key and CSR locally, places the order through the regfish TLS API, provisions the required `dns-cname-token` validation records through the regfish DNS API, waits for issuance, downloads the certificate, and deploys it to stable paths under `live/`.

For OV or business products, the TLS API can return a staged order with `action_required=true` and a `completion_url` under `/my/certs/...`. In that case, `certbro issue` still creates the private key and CSR locally, stores the pending order state, prints the Console URL, and exits successfully without blocking for issuance.

If `--validity-days` is omitted, `certbro` uses a date-aware default aligned with the current CA/B Forum validity schedule.

## OV and Business Completion Flow

Example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite
```

If you already know a usable organization id from the regfish Console, pass it up front:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite \
  --org-id 42
```

That pre-links the order to the existing organization. If the organization is already usable for ordering, the TLS API can continue directly without returning a staged completion URL.

If the TLS API responds with `action_required=true`, `certbro` prints fields such as:

- `certificate_id`
- `pending_reason`
- `pending_message`
- `completion_url`

Follow the `completion_url` in the regfish Console, complete the OV/business order there, and then rerun:

```sh
sudo certbro renew --name example-com
```

`certbro renew` resumes the same pending order and provisions DCV as soon as validation records become available. If the certificate is already ready afterwards, `certbro` downloads and deploys it in the same run. If provider-side OV/business validation is still pending, `certbro renew` exits cleanly and continues on a later renewal run or timer cycle.

## Validity Schedule

`certbro` follows the CA/B Forum schedule for publicly trusted TLS certificate lifetimes and starts using the upcoming lower default one day earlier as a safety margin.

- Certificates issued before `2026-03-14`: maximum `398` days, default `397`
- Certificates issued on or after `2026-03-14` and before `2027-03-14`: maximum `200` days, default `199`
- Certificates issued on or after `2027-03-14` and before `2029-03-14`: maximum `100` days, default `99`
- Certificates issued on or after `2029-03-14`: maximum `47` days, default `46`

If you pass `--validity-days`, `certbro` validates the requested value against the active limit before placing the order.

The purchased base lifetime must also be greater than both `--renew-before-days` and `--reissue-lead-days`. This prevents immediate follow-up renewals or reissues right after issuance.

## Subject Alternative Names

Repeat `--dns-name` for each SAN:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --dns-name api.example.com
```

The SAN list is kept in the local management state and reused for later renewals and reissues.

## Product Selection

The requested `--product` is validated against the live regfish TLS product catalog before an order is created.

Example with a non-default product:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SSL123
```

If the product does not exist, `certbro` aborts and prints the available product identifiers returned by the API.

## Key Algorithms

RSA example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --rsa-bits 3072
```

RSA is the default key type, so `--rsa-bits` is enough when you only want a non-default RSA size.

ECDSA example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --key-type ecdsa \
  --ecdsa-curve p384
```

`certbro` rotates key material on every fresh issue, renewal order, and reissue.

If you want to operate RSA and ECDSA variants in parallel for the same hostname, use [`issue-pair`](dual-certificates.md).

## Progress Output

By default, `certbro issue` prints progress updates for key phases such as product validation, DCV provisioning, waiting for issuance, and deployment.

For staged OV/business orders, the progress output changes accordingly: `certbro` reports that the order has been started, shows the Console completion URL, and does not keep waiting locally until the Console step has been completed.

For quiet automation or scripted usage, add `--quiet`:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --quiet
```
