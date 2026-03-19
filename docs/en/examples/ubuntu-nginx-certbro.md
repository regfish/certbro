# Ubuntu, nginx, regfish DNS, and certbro

German version: [../../de/examples/ubuntu-nginx-certbro.md](../../de/examples/ubuntu-nginx-certbro.md)

This example shows a complete Linux setup for `example.certbro.com` on Ubuntu:

1. Update Ubuntu and install the base packages
2. Publish a simple site for `example.certbro.com`
3. Create an `A` record for the host through the regfish DNS API
4. Install `certbro`
5. Configure `certbro`
6. Order a certificate for the host
7. Enable automatic renewals
8. Verify the renewal flow

`example.certbro.com` is only a sample hostname. Replace it with a real hostname inside a DNS zone managed through regfish DNS.

## Prerequisites

- An Ubuntu server with a public IPv4 address
- A regfish API key with access to TLS and DNS
- A DNS zone for your hostname managed through regfish DNS
- SSH access with `sudo`

API keys can be created and managed in the regfish Console at `https://dash.regfish.com/my/setting/security`.

## 1. Set the variables

Run these commands first and adjust the values:

```sh
export REGFISH_API_KEY='YOUR_REGFISH_API_KEY'
export HOST_FQDN='example.certbro.com'
export CERTBRO_NAME='example-certbro-com'
export CERTBRO_PRODUCT='RapidSSL'
export CERTBRO_VALIDITY_DAYS='3'
export WEBROOT="/var/www/${HOST_FQDN}/html"
export CERTBRO_DIR="/etc/certbro/${HOST_FQDN}"
export CERTBRO_STATE_FILE='/etc/certbro/state.json'
```

## 2. Update Ubuntu, install nginx, and enable the firewall

```sh
sudo apt-get update
sudo apt-get upgrade -y
sudo apt-get install -y nginx curl ca-certificates dnsutils ufw
sudo systemctl enable --now nginx
```

Configure `ufw` so that SSH, HTTP, and HTTPS stay reachable and all other inbound ports stay closed:

```sh
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw status verbose
```

Important warning before enabling the firewall:

- Only run `sudo ufw enable` if `OpenSSH` is already allowed.
- If you are connected through SSH and block port `22/tcp`, you can lock yourself out of the server.
- If your SSH daemon listens on a non-standard port, allow that port before enabling `ufw`.

Enable the firewall:

```sh
sudo ufw enable
sudo ufw status verbose
```

At this point:

- SSH is reachable from everywhere
- `80/tcp` and `443/tcp` are open to the public
- other inbound ports are blocked by default

## 3. Create a simple HTTP site

```sh
sudo mkdir -p "${WEBROOT}"
printf '<!doctype html><html><body><h1>%s</h1><p>nginx is ready.</p></body></html>\n' "${HOST_FQDN}" | sudo tee "${WEBROOT}/index.html" >/dev/null
```

Create an HTTP-only nginx vhost:

```sh
sudo tee "/etc/nginx/sites-available/${HOST_FQDN}" >/dev/null <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${HOST_FQDN};

    root ${WEBROOT};
    index index.html;

    location / {
        try_files \$uri \$uri/ =404;
    }
}
EOF
```

Enable the site and reload nginx:

```sh
sudo ln -sf "/etc/nginx/sites-available/${HOST_FQDN}" "/etc/nginx/sites-enabled/${HOST_FQDN}"
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl reload nginx
```

This initial `nginx -t` and reload are still manual because you are changing the nginx configuration directly before `certbro` is involved.

## 4. Create the public DNS record through the regfish DNS API

Determine the server's public IPv4 address:

```sh
export SERVER_IPV4="$(curl -4 -fsSL https://dyndns.regfish.de/ip | tr -d '\n')"
printf 'Using public IPv4: %s\n' "${SERVER_IPV4}"
```

Prepare the DNS payload:

```sh
DNS_PAYLOAD="$(cat <<EOF
{
  "type": "A",
  "name": "${HOST_FQDN}.",
  "data": "${SERVER_IPV4}",
  "ttl": 60,
  "annotation": "Web host for ${HOST_FQDN}"
}
EOF
)"
```

Use a low TTL here so that DNS changes propagate quickly during validation and issuance.

Update the record if it already exists. Create it if it does not exist yet:

The example uses a small shell function so that failures do not terminate an interactive login shell.

```sh
upsert_public_dns_record() {
  dns_tmpdir="$(mktemp -d)" || return 1

  status="$(
    curl -sS \
      -o "${dns_tmpdir}/dns-response.json" \
      -w '%{http_code}' \
      -X PATCH 'https://api.regfish.com/dns/rr' \
      -H "x-api-key: ${REGFISH_API_KEY}" \
      -H 'Accept: application/json' \
      -H 'Content-Type: application/json' \
      -d "${DNS_PAYLOAD}"
  )" || {
    rm -rf "${dns_tmpdir}"
    return 1
  }

  case "${status}" in
    200)
      cat "${dns_tmpdir}/dns-response.json"
      ;;
    404)
      curl -fsS \
        -X POST 'https://api.regfish.com/dns/rr' \
        -H "x-api-key: ${REGFISH_API_KEY}" \
        -H 'Accept: application/json' \
        -H 'Content-Type: application/json' \
        -d "${DNS_PAYLOAD}" || {
        rm -rf "${dns_tmpdir}"
        return 1
      }
      ;;
    *)
      cat "${dns_tmpdir}/dns-response.json" >&2
      rm -rf "${dns_tmpdir}"
      return 1
      ;;
  esac

  rm -rf "${dns_tmpdir}"
}

upsert_public_dns_record
unset -f upsert_public_dns_record
```

Verify DNS resolution:

```sh
dig +short "${HOST_FQDN}"
```

Wait until the hostname resolves to the server before continuing.

## 5. Install certbro

Install the latest public Linux release:

```sh
curl -fsSL https://install.certbro.com/rf | sudo sh
certbro version
```

## 6. Configure certbro

This stores the verified API key in the local certbro state:

```sh
sudo mkdir -p /etc/certbro

sudo certbro --state-file "${CERTBRO_STATE_FILE}" configure \
  --api-key "${REGFISH_API_KEY}"
```

## 7. Order the certificate

Create a certificate directory and issue the certificate:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" issue \
  --name "${CERTBRO_NAME}" \
  --common-name "${HOST_FQDN}" \
  --product "${CERTBRO_PRODUCT}" \
  --validity-days "${CERTBRO_VALIDITY_DAYS}" \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf \
  --key-type ecdsa \
  --ecdsa-curve p256 \
  --output-dir "${CERTBRO_DIR}"
```

What happens during `certbro issue`:

- `certbro` generates a fresh private key and CSR locally
- it creates the TLS order through the regfish TLS API
- it provisions the required `dns-cname-token` DCV records automatically through the regfish DNS API
- it waits for issuance
- it downloads and deploys the certificate to stable paths under `${CERTBRO_DIR}/live/`
- because `--webserver nginx` is set, it validates the nginx configuration and reloads nginx after deployment

No separate `sudo nginx -t` or `sudo systemctl reload nginx` is needed after `certbro issue` itself. The same applies to later renewals and reissues handled by `certbro`.

## 8. Switch nginx to HTTPS

Replace the HTTP-only site with an HTTP-to-HTTPS redirect plus a TLS vhost:

```sh
sudo tee "/etc/nginx/sites-available/${HOST_FQDN}" >/dev/null <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${HOST_FQDN};
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    http2 on;
    server_name ${HOST_FQDN};

    root ${WEBROOT};
    index index.html;

    ssl_certificate     ${CERTBRO_DIR}/live/fullchain.pem;
    ssl_certificate_key ${CERTBRO_DIR}/live/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;
    ssl_session_tickets off;

    location / {
        try_files \$uri \$uri/ =404;
    }
}
EOF
```

Validate and reload nginx:

```sh
sudo nginx -t
sudo systemctl reload nginx
```

> This one-time validation and reload stay manual because you are editing the nginx vhost after the certificate has already been issued. `certbro` does not currently provide a standalone command that only tests and reloads nginx without also performing a deploy step.

Verify the site:

```sh
curl -I "http://${HOST_FQDN}"
curl -I "https://${HOST_FQDN}"
```

## 9. Install the automatic renewal timer

Install the `systemd` service and timer:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" install --certificates-dir /etc/certbro
```

Check the timer:

```sh
sudo systemctl status certbro.timer --no-pager
sudo systemctl list-timers certbro.timer --all
```

## 10. Verify renewals

Check the managed certificate state:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" list
```

Run a regular renewal pass:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" renew --name "${CERTBRO_NAME}"
```

If you want to test the full renewal path immediately, you can force it:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" renew \
  --name "${CERTBRO_NAME}" \
  --force \
  --validity-days "${CERTBRO_VALIDITY_DAYS}"
```

Use `--force` only when you explicitly want to trigger a real renewal or reissue flow for testing.

Check the service logs:

```sh
sudo journalctl -u certbro.service -n 100 --no-pager
```

## Result

At the end of this walkthrough you have:

- `nginx` serving `example.certbro.com`
- a public DNS record pointing to the server
- `certbro` installed and configured
- a certificate deployed under `/etc/certbro/example.certbro.com/live/`
- hourly automatic renewal through `systemd`
