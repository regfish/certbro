# Erste Schritte

English version: [../en/getting-started.md](../en/getting-started.md)

`certbro` ist für Linux-Server ausgelegt, die die regfish TLS API und DNS API durchgängig nutzen.

## Voraussetzungen

- Linux
- Ein regfish API-Key mit Zugriff auf TLS und DNS
- Eine DNS-Zone, die über regfish DNS verwaltet wird
- `systemd`, wenn unbeaufsichtigte Renewals über `certbro install` genutzt werden sollen

API-Keys können in der regfish Console unter `https://dash.regfish.de/my/setting/security` erstellt und verwaltet werden.

## Installation

Aktuelle Release installieren:

```sh
curl -fsSL https://install.certbro.com/rf | sh
```

Bestimmte Release installieren:

```sh
curl -fsSL https://install.certbro.com/rf | CERTBRO_VERSION=v0.1.0 sh
```

## Konfiguration

Für Server-Deployments ist eine eigene State-Datei unter `/etc/certbro` sinnvoll. So bleiben Konfiguration und verwalteter Zertifikatszustand an einem Ort:

```sh
sudo mkdir -p /etc/certbro

sudo certbro --state-file /etc/certbro/state.json configure \
  --api-key YOUR_REGFISH_API_KEY
```

`certbro configure` validiert den API-Key, bevor er gespeichert wird. Befehle mit API-Zugriff laufen nur mit einem verifizierten konfigurierten Key.

## Erstes Zertifikat bestellen

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --webserver nginx \
  --output-dir /etc/certbro/example.com
```

Wenn `--validity-days` nicht gesetzt ist, wählt `certbro` automatisch einen datumsabhängigen Default gemäß CA/B-Forum-Zeitplan. Die aktuellen Defaults sind `199` Tage ab `2026-03-15`, `99` Tage ab `2027-03-15` und `46` Tage ab `2029-03-15`.

Nach erfolgreicher Bestellung schreibt `certbro` unter anderem:

- `/etc/certbro/example.com/certbro.json`
- `/etc/certbro/example.com/live/fullchain.pem`
- `/etc/certbro/example.com/live/cert.pem`
- `/etc/certbro/example.com/live/chain.pem`
- `/etc/certbro/example.com/live/privkey.pem`
- `/etc/certbro/example.com/live/request.csr.pem`
- `/etc/certbro/example.com/live/metadata.json`
- `/etc/certbro/example.com/archive/<timestamp>/...`

Wenn die Bestellung noch `pending` ist, bleibt temporärer Order-Zustand unter `/etc/certbro/example.com/pending/` erhalten. Spätere `certbro renew`-Läufe setzen diesen Vorgang automatisch fort.

## Webserver-Integration

`certbro` funktioniert am besten, wenn der Webserver auf die stabilen Dateien unter `live/` zeigt.

Unterstütztes eingebautes Verhalten für Validierung und Reload:

- `nginx`: mit `nginx -t` prüfen, dann reload
- `apache`: mit `apachectl -t` oder `apache2ctl -t` prüfen, dann graceful reload
- `caddy`: mit `caddy validate --config ...` prüfen, dann reload

Beispiel:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf \
  --output-dir /etc/certbro/example.com
```

## Nächste Schritte

- [Zertifikate bestellen](issuing-certificates.md)
- [Parallele RSA- und ECDSA-Zertifikate](dual-certificates.md)
- [Renewals und Ersatz](renewals-and-replacement.md)
- [Linux-Automatisierung](linux-automation.md)
- [Ubuntu + nginx Beispiel](examples/ubuntu-nginx-certbro.md)
- [Ubuntu + Apache Beispiel](examples/ubuntu-apache-certbro.md)
