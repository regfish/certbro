# certbro

English version: [README.md](README.md)

`certbro` ist eine Open-Source-CLI für Linux auf Basis der regfish TLS API und DNS API.

Das Tool bestellt Zertifikate, legt `dns-cname-token`-Validierungsrecords über regfish DNS an, lädt ausgestellte Zertifikate herunter, rotiert Schlüsselmaterial, deployt stabile PEM-Pfade und speichert genug lokalen Zustand für unbeaufsichtigte Verlängerungen.

## Warum certbro

- Eine Binary und ein kurzer Installer
- Native Integration für regfish TLS und DNS
- Automatisches DCV-Provisioning über regfish DNS
- RSA- und ECDSA-Key-Rotation bei neuen Bestellungen, Renewal-Orders und Reissues
- Stabile Dateien unter `live/` und versionierte Snapshots unter `archive/`
- Eingebaute Validierung und Reload-Unterstützung für `nginx`, `apache` und `caddy`
- Unbeaufsichtigte Renewals über `systemd`

## Voraussetzungen

- Linux
- Ein regfish API-Key mit Zugriff auf TLS und DNS
- Eine DNS-Zone, die über regfish DNS verwaltet wird
- `systemd`, wenn `certbro install` genutzt werden soll

API-Keys können in der regfish Console unter `https://dash.regfish.de/my/setting/security` erstellt und verwaltet werden.

## Installation

Aktuelle Linux-Release installieren:

```sh
curl -fsSL https://install.certbro.com/rf | sh
```

Bestimmte Release installieren:

```sh
curl -fsSL https://install.certbro.com/rf | CERTBRO_VERSION=v0.1.0 sh
```

## Schnellstart

Standardmäßig nutzt `certbro` `/etc/certbro/state.json` als State-Datei und `/etc/certbro` als Root für verwaltete Zertifikate. `issue`, `import` und `issue-pair` leiten Zertifikatsverzeichnisse ebenfalls aus diesem Root und dem `common-name` ab. Die folgenden Befehle verwenden diese Defaults. `--state-file`, `--certificates-dir`, `--output-dir` und `--output-dir-base` sind nur nötig, wenn andere Pfade genutzt werden sollen.

```sh
sudo mkdir -p /etc/certbro

sudo certbro configure --api-key YOUR_REGFISH_API_KEY
```

Zertifikat bestellen und deployen:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --webserver nginx
```

Für DV-Produkte wartet `certbro issue` in der Regel auf die Ausstellung und deployt das Zertifikat direkt. Für OV- oder organisationsvalidierte Business-Produkte kann die TLS API stattdessen `action_required=true` mit einer `completion_url` unter `/my/certs/...` zurückgeben. In diesem Fall speichert `certbro` das Pending-Material lokal, gibt die Console-URL aus und endet erfolgreich. Ein späterer Lauf von `certbro renew` setzt denselben offenen Vorgang fort, nachdem der Console-Schritt abgeschlossen wurde.

Renewals manuell ausführen:

```sh
sudo certbro renew
```

Stündlichen Renewal-Timer installieren:

```sh
sudo certbro install
```

## Typische Workflows

- Multi-Domain-Zertifikate: `--dns-name` für jede SAN wiederholen
- Paralleler RSA- und ECDSA-Betrieb: `certbro issue-pair`
- Bereits bestehende regfish-Bestellungen: Import per `certificate_id`
- OV- oder Business-Bestellungen: `--org-id` erwartet die öffentliche TLS-Organisations-ID aus `/tls/organization` beziehungsweise `organization_id`, zum Beispiel `hdl_ABCDEFGHJKLMN`; `certbro issue` kann trotzdem noch eine Console-`completion_url` zurückgeben, wenn die Organisation nicht bestellbar ist oder weitere Completion-Daten fehlen
- Sofortiger Ersatz: `certbro renew --name example-com --force`
- Einmaliger Laufzeit-Override: `certbro renew --name example-com --force --validity-days 30`
- Pending-Ausstellung nach Timeout: `certbro renew --name example-com` erneut ausführen, um denselben Request weiter zu beobachten
- Wenn `--validity-days` fehlt, verwendet `certbro` einen datumsabhängigen Default mit einem Sicherheitspuffer von einem Tag vor jedem CA/B-Forum-Stichtag: `199` Tage ab 2026-03-14, `99` Tage ab 2027-03-14 und `46` Tage ab 2029-03-14
- Ruhige Ausgabe für Automatisierung: `--quiet` bei `issue` oder `renew`

## Dokumentation

- [Dokumentationssprachen](docs/README.md)
- [Dokumentation auf Deutsch](docs/de/README.md)
- [Documentation in English](docs/en/README.md)

## Community

- [Contributing](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)

## Build From Source

```sh
go build ./cmd/certbro
```

## Tests

Vollständigen Preflight vor Commit oder Release ausführen:

```sh
./scripts/test-preflight.sh
```

Nur den CLI-Smoke-Test ausführen:

```sh
./scripts/test-smoke.sh
```

## Lizenz

Dieses Projekt ist unter der Apache License 2.0 lizenziert. Siehe [LICENSE](LICENSE).
