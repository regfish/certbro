# Dual RSA and ECDSA Certificates

German version: [../de/dual-certificates.md](../de/dual-certificates.md)

`certbro` can manage two certificate variants for the same hostname in parallel:

- one RSA certificate for broad client compatibility
- one ECDSA certificate for modern clients that prefer smaller keys and faster handshakes

This is the standard way to serve both key types from the same `nginx` or Apache virtual host.

## Issue a Matching Pair

`certbro issue-pair` creates two managed certificates with consistent naming:

- `<name-base>-rsa`
- `<name-base>-ecdsa`

It also derives matching output directories:

- `<output-dir-base>-rsa`
- `<output-dir-base>-ecdsa`

Example:

```sh
sudo certbro --state-file /etc/certbro/state.json issue-pair \
  --name-base example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --product RapidSSL \
  --output-dir-base /etc/certbro/example.com \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf
```

This creates and manages:

- `/etc/certbro/example.com-rsa`
- `/etc/certbro/example.com-ecdsa`

`certbro issue-pair` issues the RSA variant first, then the ECDSA variant. The webserver reload happens only after both certificate directories exist.

## nginx Configuration

Point `nginx` at both certificate pairs:

```nginx
ssl_certificate     /etc/certbro/example.com-ecdsa/live/fullchain.pem;
ssl_certificate_key /etc/certbro/example.com-ecdsa/live/privkey.pem;

ssl_certificate     /etc/certbro/example.com-rsa/live/fullchain.pem;
ssl_certificate_key /etc/certbro/example.com-rsa/live/privkey.pem;
```

With this configuration, TLS clients can negotiate the most suitable certificate automatically.

## Renewals

Both managed certificates remain independent renewal units. They can be renewed:

- together via `certbro renew`
- together via `certbro --certificates-dir /etc/certbro renew`
- individually via `certbro renew --name example-com-rsa` and `certbro renew --name example-com-ecdsa`

Example:

```sh
sudo certbro --state-file /etc/certbro/state.json renew --name example-com-rsa
sudo certbro --state-file /etc/certbro/state.json renew --name example-com-ecdsa
```

If both certificates are configured with `--webserver nginx`, each successful renewal validates and reloads `nginx` after deployment.

## Notes

- The RSA and ECDSA variants are stored as two separate managed certificates
- Each variant has its own `certificate_id`
- Each variant can be renewed or replaced independently
- `issue-pair` is currently optimized for Linux server deployments, especially with `nginx`
