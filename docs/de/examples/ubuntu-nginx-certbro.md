# Ubuntu, nginx, regfish DNS und certbro

English version: [../../en/examples/ubuntu-nginx-certbro.md](../../en/examples/ubuntu-nginx-certbro.md)

Dieses Beispiel zeigt einen kompletten Linux-Workflow für `example.certbro.com` auf Ubuntu:

1. Ubuntu aktualisieren und Basispakete installieren
2. Eine einfache Website für `example.certbro.com` veröffentlichen
3. Einen `A`-Record für den Host über die regfish DNS API anlegen
4. `certbro` installieren
5. `certbro` konfigurieren
6. Ein Zertifikat für den Host bestellen
7. Automatische Renewals aktivieren
8. Den Renewal-Flow prüfen

`example.certbro.com` ist nur ein Beispiel-Hostname. Ersetze ihn durch einen echten Hostnamen innerhalb einer DNS-Zone, die über regfish DNS verwaltet wird.

## Voraussetzungen

- Ein Ubuntu-Server mit öffentlicher IPv4-Adresse
- Ein regfish API-Key mit Zugriff auf TLS und DNS
- Eine DNS-Zone für deinen Hostnamen, die über regfish DNS verwaltet wird
- SSH-Zugriff mit `sudo`

API-Keys können in der regfish Console unter `https://dash.regfish.de/my/setting/security` erstellt und verwaltet werden.

## 1. Variablen setzen

Diese Kommandos zuerst ausführen und die Werte anpassen:

```sh
export REGFISH_API_KEY='YOUR_REGFISH_API_KEY'
export HOST_FQDN='example.certbro.com'
export CERTBRO_NAME='example-certbro-com'
export CERTBRO_PRODUCT='RapidSSL'
export CERTBRO_VALIDITY_DAYS='3'
export CERTBRO_RENEW_BEFORE_DAYS='2'
export CERTBRO_REISSUE_LEAD_DAYS='2'
export WEBROOT="/var/www/${HOST_FQDN}/html"
export CERTBRO_DIR="/etc/certbro/${HOST_FQDN}"
export CERTBRO_STATE_FILE='/etc/certbro/state.json'
```

## 2. Ubuntu aktualisieren, nginx installieren und Firewall aktivieren

```sh
sudo apt-get update
sudo apt-get upgrade -y
sudo apt-get install -y nginx curl ca-certificates dnsutils ufw
sudo systemctl enable --now nginx
```

`ufw` so konfigurieren, dass SSH, HTTP und HTTPS erreichbar bleiben und alle anderen eingehenden Ports geschlossen sind:

```sh
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw status verbose
```

Wichtiger Warnhinweis vor dem Aktivieren der Firewall:

- `sudo ufw enable` nur ausführen, wenn `OpenSSH` bereits erlaubt ist
- Wenn du per SSH verbunden bist und `22/tcp` blockierst, kannst du dich selbst aussperren
- Wenn dein SSH-Daemon auf einem anderen Port lauscht, diesen Port vorher freigeben

Firewall aktivieren:

```sh
sudo ufw enable
sudo ufw status verbose
```

Danach gilt:

- SSH ist von überall erreichbar
- `80/tcp` und `443/tcp` sind öffentlich offen
- andere eingehende Ports sind standardmäßig blockiert

## 3. Eine einfache HTTP-Site anlegen

```sh
sudo mkdir -p "${WEBROOT}"
printf '<!doctype html><html><body><h1>%s</h1><p>nginx is ready.</p></body></html>\n' "${HOST_FQDN}" | sudo tee "${WEBROOT}/index.html" >/dev/null
```

Einen reinen HTTP-nginx-vHost anlegen:

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

Site aktivieren und nginx neu laden:

```sh
sudo ln -sf "/etc/nginx/sites-available/${HOST_FQDN}" "/etc/nginx/sites-enabled/${HOST_FQDN}"
sudo rm -f /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl reload nginx
```

Dieses erste `nginx -t` und der Reload bleiben manuell, weil die nginx-Konfiguration direkt geändert wird, bevor `certbro` beteiligt ist.

## 4. Öffentlichen DNS-Record über die regfish DNS API anlegen

Öffentliche IPv4-Adresse des Servers bestimmen:

```sh
export SERVER_IPV4="$(curl -4 -fsSL https://dyndns.regfish.de/ip | tr -d '\n')"
printf 'Using public IPv4: %s\n' "${SERVER_IPV4}"
```

DNS-Payload vorbereiten:

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

Hier bewusst ein niedriges TTL verwenden, damit DNS-Änderungen während Validierung und Ausstellung möglichst schnell wirksam werden.

Wenn der Record bereits existiert, aktualisieren. Falls nicht, neu anlegen:

Das Beispiel verwendet eine kleine Shell-Funktion, damit Fehler keine interaktive Login-Shell beenden.

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

DNS-Auflösung prüfen:

```sh
dig +short "${HOST_FQDN}"
```

Warten, bis der Hostname auf die Server-IP auflöst, bevor es weitergeht.

## 5. certbro installieren

Aktuelle öffentliche Linux-Release installieren:

```sh
curl -fsSL https://install.certbro.com/rf | sudo sh
certbro version
```

## 6. certbro konfigurieren

Dadurch wird der verifizierte API-Key im lokalen certbro-State gespeichert:

```sh
sudo mkdir -p /etc/certbro

sudo certbro --state-file "${CERTBRO_STATE_FILE}" configure \
  --api-key "${REGFISH_API_KEY}"
```

## 7. Zertifikat bestellen

Zertifikatsverzeichnis anlegen und Zertifikat bestellen:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" issue \
  --name "${CERTBRO_NAME}" \
  --common-name "${HOST_FQDN}" \
  --product "${CERTBRO_PRODUCT}" \
  --validity-days "${CERTBRO_VALIDITY_DAYS}" \
  --renew-before-days "${CERTBRO_RENEW_BEFORE_DAYS}" \
  --reissue-lead-days "${CERTBRO_REISSUE_LEAD_DAYS}" \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf \
  --key-type ecdsa \
  --ecdsa-curve p256 \
  --output-dir "${CERTBRO_DIR}"
```

Was während `certbro issue` passiert:

- `certbro` erzeugt lokal einen frischen Private Key und CSR
- es legt die TLS-Bestellung über die regfish TLS API an
- es provisioniert automatisch die benötigten `dns-cname-token`-DCV-Records über die regfish DNS API
- es wartet auf die Ausstellung
- es lädt das Zertifikat herunter und deployt es auf stabile Pfade unter `${CERTBRO_DIR}/live/`
- weil `--webserver nginx` gesetzt ist, validiert es die nginx-Konfiguration und reloadet nginx nach dem Deployment

Nach `certbro issue` selbst ist kein separates `sudo nginx -t` oder `sudo systemctl reload nginx` mehr nötig. Dasselbe gilt für spätere Renewals und Reissues, die von `certbro` ausgeführt werden.

## 8. nginx auf HTTPS umstellen

Die reine HTTP-Site durch Redirect plus TLS-vHost ersetzen:

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

nginx prüfen und reloaden:

```sh
sudo nginx -t
sudo systemctl reload nginx
```

> Diese einmalige Validierung und dieser Reload bleiben manuell, weil der nginx-vHost nach der Zertifikatsausstellung noch einmal von Hand geändert wird. `certbro` hat derzeit keinen eigenständigen Befehl, der nur `nginx` testet und reloadet, ohne zugleich einen Deploy-Schritt auszuführen.

Site prüfen:

```sh
curl -I "http://${HOST_FQDN}"
curl -I "https://${HOST_FQDN}"
```

## 9. Automatischen Renewal-Timer installieren

`systemd`-Service und Timer installieren:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" install --certificates-dir /etc/certbro
```

Timer prüfen:

```sh
sudo systemctl status certbro.timer --no-pager
sudo systemctl list-timers certbro.timer --all
```

## 10. Renewals prüfen

Verwalteten Zertifikatszustand anzeigen:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" list
```

Regulären Renewal-Lauf starten:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" renew --name "${CERTBRO_NAME}"
```

Wenn der komplette Renewal-Pfad sofort getestet werden soll, kann er erzwungen werden:

```sh
sudo certbro --state-file "${CERTBRO_STATE_FILE}" renew \
  --name "${CERTBRO_NAME}" \
  --force \
  --validity-days "${CERTBRO_VALIDITY_DAYS}"
```

Das `3`-Tage-Beispiel funktioniert nur, weil die gespeicherten Lead-Tage auf `2` gesetzt wurden. `certbro` lehnt Laufzeiten ab, die nicht größer sind als die Renewal-Lead-Tage.

`--force` nur verwenden, wenn bewusst ein echter Renewal- oder Reissue-Flow für Tests ausgelöst werden soll.

Service-Logs prüfen:

```sh
sudo journalctl -u certbro.service -n 100 --no-pager
```

## Ergebnis

Am Ende dieses Walkthroughs gibt es:

- `nginx`, das `example.certbro.com` ausliefert
- einen öffentlichen DNS-Record, der auf den Server zeigt
- ein installiertes und konfiguriertes `certbro`
- ein Zertifikat unter `/etc/certbro/example.certbro.com/live/`
- stündliche automatische Renewals über `systemd`
