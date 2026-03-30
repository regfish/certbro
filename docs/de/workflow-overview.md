# Workflow-Überblick

English version: [../en/workflow-overview.md](../en/workflow-overview.md)

Dieses Dokument erklärt, wie `certbro`, die regfish TLS API, die regfish Console und die regfish DNS API bei unterschiedlichen Zertifikatstypen zusammenspielen.

## DV-Bestellablauf

Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com
```

Typischer Ablauf:

1. `certbro issue` erzeugt Private Key und CSR lokal.
2. Die Bestellung wird über `POST /tls/certificate` angelegt.
3. Für DV-Produkte erzeugt die TLS API in der Regel sofort die Provider-Order und liefert `validation.dns_records` direkt in derselben Antwort zurück.
4. `certbro` legt diese `dns-cname-token`-Records über die regfish DNS API an.
5. `certbro issue` wartet auf die Ausstellung, lädt das Zertifikat herunter, deployt es und reloadet den konfigurierten Webserver.

In diesem Ablauf passiert die DNS-Validierung direkt im ersten `issue`-Lauf.

## Gestufter OV- und Business-Flow

Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite
```

Typischer gestufter Ablauf:

1. `certbro issue` erzeugt weiterhin Private Key und CSR lokal und schickt den Bestellwunsch an die TLS API.
2. Wenn noch Organisations- oder Completion-Daten fehlen, antwortet die TLS API zunächst nur mit einer gestuften lokalen Resource mit `action_required=true` und einer `completion_url` unter `/my/certs/...`.
3. `certbro` speichert das Pending-Keymaterial und die Bestellmetadaten lokal und beendet sich erfolgreich.
4. Es werden noch keine DNS-Validierungsrecords angelegt, weil die gestufte Resource zu diesem Zeitpunkt noch keine `validation.dns_records` bereitstellt.
5. Die `completion_url` wird in der regfish Console geöffnet und die OV-/Business-Bestellung dort abgeschlossen.
6. Ein späterer Lauf von `certbro renew` lädt dieselbe Zertifikats-Resource erneut über `GET /tls/certificate/{certificate_id}`.
7. Sobald die TLS API `action_required=false` und einen `validation`-Block mit `dns_records` zurückgibt, legt `certbro renew` die CNAMEs über die regfish DNS API an.
8. Wenn das Zertifikat danach schon bereitsteht, lädt `certbro renew` es im selben Lauf herunter, deployt es und entfernt den Pending-Zustand.
9. Wenn die providerseitige OV-/Business-Validierung noch läuft, endet `certbro renew` nach dem DCV-Schritt sauber und setzt den Vorgang erst bei einem späteren Renewal-Lauf oder Timer-Zyklus fort.

Wichtiger Unterschied zu DV:

- Nicht die Console legt die DCV-CNAMEs an.
- Das macht weiterhin `certbro`.
- Der Unterschied ist nur das Timing: Bei gestuften OV-/Business-Bestellungen werden die CNAMEs später im `renew`-Pfad angelegt, nachdem der Console-Schritt abgeschlossen wurde und die TLS API die Validierungsdaten tatsächlich bereitstellt.
- Sobald diese CNAMEs gesetzt sind, geht `certbro` nicht mehr davon aus, dass die Ausstellung sofort folgen muss. Die OV-/Business-Freigabe kann asynchron weiterlaufen.

## OV und Business mit vorhandener Organisation

Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite \
  --org-id hdl_ABCDEFGHJKLMN
```

Dadurch ändert sich der Ablauf:

1. `certbro` erzeugt Private Key und CSR weiterhin lokal.
2. Die Bestellung wird direkt mit der vorhandenen öffentlichen TLS-Organisations-ID (`org_id`, zum Beispiel `hdl_ABCDEFGHJKLMN`) verknüpft.
3. Ist diese Organisation bereits bestellbar, kann die TLS API ohne den gestuften `completion_url`-Zwischenschritt direkt weiterlaufen.
4. Dann nähert sich der Ablauf wieder dem DV-Fall an: Validierungsrecords können schon in der ersten `issue`-Antwort auftauchen, und `certbro issue` kann die DNS-Records direkt im selben Lauf anlegen.
5. Ist die Organisation unvollständig oder nicht bestellbar, kann die TLS API trotzdem wieder in den gestuften OV-/Business-Flow zurückfallen.

Wichtig für API und CLI:

- `POST /tls/certificate` nimmt optional `org_id`.
- `POST /tls/certificate/{certificate_id}/complete` verlangt `org_id` zwingend.
- Die Response-Felder `organization_id` und `organization.id` verwenden dieselbe öffentliche string-basierte TLS-Organisations-ID.
- Ältere numerische Beispiele sind dafür nicht mehr maßgeblich.

## Was `renew` bei Pending-Bestellungen macht

`certbro renew` ist der einzige Resume- und Finalize-Mechanismus für offene Bestellungen.

Der Ablauf ist:

1. Meldet das Zertifikat weiterhin `action_required=true`, erstellt `renew` keine Doppelbestellung und wartet lokal nicht weiter. Es zeigt nur die gespeicherte oder aktuelle `completion_url`.
2. Meldet das Zertifikat `action_required=false` und liegen Validierungsrecords vor, legt `renew` die DNS-Records an und fährt fort.
3. Ist das Zertifikat bereits ausgestellt, lädt `renew` es herunter und deployt es.

Bei gestuften OV-/Business-Bestellungen kann Schritt 2 auch mit "DCV provisioniert, providerseitige Validierung läuft noch" enden. Das ist ein sauberer Zwischenzustand und kein Fehler.

Genau derselbe Befehl wird auch vom stündlichen `systemd`-Timer genutzt. Es gibt keinen separaten OV-Timer.

## Wann die DCV-Records auftauchen

Bei gestuften OV-/Business-Bestellungen werden die Validierungsrecords erst dann verfügbar, wenn der Console-Abschluss aus der gestuften Resource eine echte Provider-Order gemacht hat und die TLS API daraus `validation` ableiten kann.

Das bedeutet:

- vor dem Console-Abschluss: `action_required=true`, keine nutzbaren DNS-Validierungsdaten
- nach dem Console-Abschluss: `action_required=false`, später `validation.dns_records`
- sobald `validation.dns_records` vorhanden sind: `certbro` kann die CNAMEs anlegen

`certbro renew` prüft die Validierungsdaten auch während seiner Polling-Schleife. Wenn der erste Poll nach dem Console-Abschluss noch keinen DNS-Token enthält, aber ein späterer Poll im selben Lauf schon, kann `certbro` die CNAMEs noch innerhalb desselben `renew`-Laufs anlegen, ohne auf den nächsten Timer zu warten.

## Verantwortlichkeiten

- `certbro`: Key-Erzeugung, CSR-Erzeugung, lokaler Zustand, DCV-CNAME-Provisioning, Download, Deployment
- regfish TLS API: Bestellstatus, Provider-Anbindung, Validierungsanweisungen
- regfish Console: menschlicher Completion-Schritt bei gestuften OV-/Business-Bestellungen
- regfish DNS API: tatsächliches Anlegen und Aufräumen der DNS-Records durch `certbro`
