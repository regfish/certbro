# certbro

German version: [README.de.md](README.de.md)

`certbro` is an open source Linux CLI for the regfish TLS API and DNS API.

It orders certificates, provisions `dns-cname-token` validation records through regfish DNS, downloads issued certificates, rotates keys, deploys stable PEM paths, and keeps enough local state for unattended renewals.

## Why certbro

- One binary and one short installer
- Native regfish TLS and DNS integration
- Automatic DCV provisioning through regfish DNS
- RSA and ECDSA key rotation on new issues, renewal orders, and reissues
- Stable files under `live/` and versioned snapshots under `archive/`
- Built-in validation and reload support for `nginx`, `apache`, and `caddy`
- Hourly unattended renewals through `systemd`

## Requirements

- Linux
- A regfish API key with access to TLS and DNS
- A DNS zone managed through regfish DNS
- `systemd` if you want `certbro install`

API keys can be created and managed in the regfish Console at `https://dash.regfish.com/my/setting/security`.

## Install

Install the latest Linux release:

```sh
curl -fsSL https://install.certbro.com/rf | sh
```

Install a specific release:

```sh
curl -fsSL https://install.certbro.com/rf | CERTBRO_VERSION=v0.1.0 sh
```

## Quick Start

Use a system-wide state file for server deployments:

```sh
sudo mkdir -p /etc/certbro

sudo certbro --state-file /etc/certbro/state.json configure \
  --api-key YOUR_REGFISH_API_KEY
```

Issue and deploy a certificate:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --product RapidSSL \
  --webserver nginx \
  --key-type ecdsa \
  --ecdsa-curve p256 \
  --output-dir /etc/certbro/example.com
```

Run renewals manually:

```sh
sudo certbro --state-file /etc/certbro/state.json renew
```

Install the hourly renewal timer:

```sh
sudo certbro --state-file /etc/certbro/state.json install \
  --certificates-dir /etc/certbro
```

## Common Workflows

- Multi-domain certificates: repeat `--dns-name` for each SAN
- Dual RSA and ECDSA deployment: use `certbro issue-pair`
- Existing regfish orders: import by `certificate_id`
- Immediate replacement: `certbro renew --name example-com --force`
- One-off renewal lifetime override: `certbro renew --name example-com --force --validity-days 3`
- Pending issuance after a timeout: rerun `certbro renew --name example-com` to resume monitoring the existing request
- If `--validity-days` is omitted, `certbro` uses a date-aware default aligned with the CA/B Forum validity schedule: `199` days from 2026-03-15, `99` days from 2027-03-15, and `46` days from 2029-03-15
- Quiet output for automation: add `--quiet` to `issue` or `renew`

## Documentation

- [Documentation Languages](docs/README.md)
- [Documentation in English](docs/en/README.md)
- [Erste Schritte auf Deutsch](docs/de/README.md)

## Community

- [Contributing](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)

## Build From Source

```sh
go build ./cmd/certbro
```

## Testing

Run the full preflight before a commit or release:

```sh
./scripts/test-preflight.sh
```

Run only the CLI smoke test:

```sh
./scripts/test-smoke.sh
```

## License

This project is licensed under the Apache License 2.0. See [LICENSE](LICENSE).
