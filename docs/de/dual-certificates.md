# Parallele RSA- und ECDSA-Zertifikate

English version: [../en/dual-certificates.md](../en/dual-certificates.md)

`certbro` kann zwei Zertifikatsvarianten für denselben Hostnamen parallel verwalten:

- ein RSA-Zertifikat für breite Client-Kompatibilität
- ein ECDSA-Zertifikat für moderne Clients mit kleineren Schlüsseln und schnelleren Handshakes

Das ist die übliche Vorgehensweise, um beide Key-Typen aus demselben `nginx`- oder Apache-vHost auszuliefern.

## Passendes Paar bestellen

`certbro issue-pair` erzeugt zwei verwaltete Zertifikate mit konsistenter Benennung:

- `<name-base>-rsa`
- `<name-base>-ecdsa`

Auch die Verzeichnisse werden daraus konsistent abgeleitet:

- `<output-dir-base>-rsa`
- `<output-dir-base>-ecdsa`

Wenn `--output-dir-base` fehlt, nutzt `certbro issue-pair` zunächst `<certificates-dir>/<common-name>` und hängt danach diese Suffixe an.

Beispiel:

```sh
sudo certbro issue-pair \
  --name-base example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --webserver nginx \
  --webserver-config /etc/nginx/nginx.conf
```

Dadurch entstehen und werden verwaltet:

- `/etc/certbro/example.com-rsa`
- `/etc/certbro/example.com-ecdsa`

`certbro issue-pair` bestellt zuerst die RSA-Variante und danach die ECDSA-Variante. Der Webserver-Reload erfolgt erst, wenn beide Zertifikatsverzeichnisse vorhanden sind.

## nginx-Konfiguration

`nginx` auf beide Zertifikatspaare zeigen lassen:

```nginx
ssl_certificate     /etc/certbro/example.com-ecdsa/live/fullchain.pem;
ssl_certificate_key /etc/certbro/example.com-ecdsa/live/privkey.pem;

ssl_certificate     /etc/certbro/example.com-rsa/live/fullchain.pem;
ssl_certificate_key /etc/certbro/example.com-rsa/live/privkey.pem;
```

Mit dieser Konfiguration handeln TLS-Clients automatisch das jeweils passende Zertifikat aus.

## Renewals

Beide Zertifikate bleiben eigenständige Renewal-Einheiten. Sie können:

- gemeinsam über `certbro renew`
- einzeln über `certbro renew --name example-com-rsa` und `certbro renew --name example-com-ecdsa`

erneuert werden.

Wenn das Paar unter einem nicht-defaultigen Root liegt, einfach zusätzlich `--certificates-dir /pfad/zu/certbro` setzen.

Beispiel:

```sh
sudo certbro renew --name example-com-rsa
sudo certbro renew --name example-com-ecdsa
```

Wenn beide Zertifikate mit `--webserver nginx` konfiguriert sind, validiert und reloadet jeder erfolgreiche Renewal-Lauf `nginx` nach dem Deployment.

## Hinweise

- RSA- und ECDSA-Variante werden als zwei getrennte Managed-Certificate-Einträge gespeichert
- Jede Variante hat ihre eigene `certificate_id`
- Jede Variante kann unabhängig renewed oder ersetzt werden
- `issue-pair` ist aktuell besonders für Linux-Server und `nginx` optimiert
