# Dokumentation

English version: [../en/README.md](../en/README.md)

Dieses Verzeichnis enthält die deutschsprachigen Betriebs- und Nutzungshinweise für `certbro`.

## Einstieg

- [Erste Schritte](getting-started.md): `certbro` installieren, API-Key verifizieren und das erste Zertifikat bestellen
- [Linux-Automatisierung](linux-automation.md): stündlichen `systemd`-Timer für Renewals installieren und betreiben

## Zertifikats-Workflows

- [Zertifikate bestellen](issuing-certificates.md): Single-Domain, SANs, Produktauswahl, Key-Algorithmen, Laufzeitplan und leise Ausgabe
- [Parallele RSA- und ECDSA-Zertifikate](dual-certificates.md): gleichzeitiger Betrieb für moderne und ältere TLS-Clients
- [Renewals und Ersatz](renewals-and-replacement.md): reguläre Renewals, erzwungene Renewals, Laufzeit-Overrides, künftige CA/B-Forum-Limits und schneller Ersatz
- [Bestehende Zertifikate importieren](import-existing-certificates.md): Zertifikate aus der regfish UI unter `certbro`-Verwaltung übernehmen

## Beispiele

- [Ubuntu + nginx Beispiel](examples/ubuntu-nginx-certbro.md): kompletter Server-Workflow mit DNS, `nginx`, `ufw`, `certbro` und automatischen Renewals
- [Ubuntu + Apache Beispiel](examples/ubuntu-apache-certbro.md): kompletter Server-Workflow mit DNS, `apache2`, `ufw`, `certbro` und automatischen Renewals
