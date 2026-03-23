# Getting Started

German version: [../de/getting-started.md](../de/getting-started.md)

`certbro` is designed for Linux servers that use the regfish TLS API and DNS API end to end.

## Requirements

- Linux
- A regfish API key with access to TLS and DNS
- A DNS zone managed through regfish DNS
- `systemd` if you want unattended renewals through `certbro install`

API keys can be created and managed in the regfish Console at `https://dash.regfish.com/my/setting/security`.

## Install

Install the latest release:

```sh
curl -fsSL https://install.certbro.com/rf | sh
```

Install a specific release:

```sh
curl -fsSL https://install.certbro.com/rf | CERTBRO_VERSION=v0.1.0 sh
```

## Configure

By default, `certbro` uses `/etc/certbro/state.json` and `/etc/certbro`. For server deployments, these defaults keep configuration and managed certificate state in one place. The commands below rely on them:

```sh
sudo mkdir -p /etc/certbro

sudo certbro configure --api-key YOUR_REGFISH_API_KEY
```

`certbro configure` validates the API key before it is stored. Commands that talk to the regfish API require a verified configured key.

## Issue the First Certificate

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --webserver nginx
```

Without an explicit `--output-dir`, `certbro` writes to `/etc/certbro/example.com` under the Linux defaults.

If `--validity-days` is omitted, `certbro` chooses a date-aware default with a one-day safety margin before each CA/B Forum transition. The current defaults are `199` days from `2026-03-14`, `99` days from `2027-03-14`, and `46` days from `2029-03-14`.

After a successful issue, `certbro` writes:

- `/etc/certbro/example.com/certbro.json`
- `/etc/certbro/example.com/live/fullchain.pem`
- `/etc/certbro/example.com/live/cert.pem`
- `/etc/certbro/example.com/live/chain.pem`
- `/etc/certbro/example.com/live/privkey.pem`
- `/etc/certbro/example.com/live/request.csr.pem`
- `/etc/certbro/example.com/live/metadata.json`
- `/etc/certbro/example.com/archive/<timestamp>/...`

If the order is still pending, temporary order state remains under `/etc/certbro/example.com/pending/` and later `certbro renew` runs resume it automatically. For OV or business orders, this pending state can also contain a Console `completion_url` under `/my/certs/...`; complete that step there and let `certbro renew` finalize the same order afterwards.

## Webserver Integration

`certbro` works best when the webserver points to the stable files under `live/`.

Supported built-in validation and reload behavior:

- `nginx`: validate with `nginx -t`, then reload
- `apache`: validate with `apachectl -t` or `apache2ctl -t`, then graceful reload
- `caddy`: validate with `caddy validate --config ...`, then reload

Example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf
```

## Next Steps

- [Issuing Certificates](issuing-certificates.md)
- [Dual RSA and ECDSA Certificates](dual-certificates.md)
- [Renewals and Replacement](renewals-and-replacement.md)
- [Linux Automation](linux-automation.md)
- [Ubuntu + nginx Example](examples/ubuntu-nginx-certbro.md)
- [Ubuntu + Apache Example](examples/ubuntu-apache-certbro.md)
