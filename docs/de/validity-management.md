# Laufzeitverwaltung

English version: [../en/validity-management.md](../en/validity-management.md)

## Überblick

`certbro` speichert die gekaufte Basislaufzeit pro verwaltetem Zertifikat und verwendet sie für künftige Renewal-Orders und neue Orders weiter.

Für `example.com` kann diese gespeicherte Laufzeit jederzeit geändert werden. `certbro` verwendet dann bei späteren Renewals den neuen Wert als gekaufte Basislaufzeit, solange er innerhalb des aktuell gültigen schedule-aware Limits liegt.

## Gespeicherte Laufzeit manuell ändern

Beispiel: `example.com` wurde ursprünglich mit `3` Tagen bestellt und soll künftig `30` Tage verwenden.

Gespeicherten Wert aktualisieren:

```sh
sudo certbro update --name example-com --validity-days 30
```

Danach das nächste Renewal normal ausführen:

```sh
sudo certbro renew --name example-com
```

Wenn das aktuell deployte Zertifikat sofort mit der neuen gewünschten Laufzeit ersetzt werden soll:

```sh
sudo certbro renew --name example-com --force --validity-days 30
```

Für sehr kurz laufende Zertifikate müssen die Lead-Tage unter der gewünschten Laufzeit bleiben. Beispiel für ein Zertifikat mit `3` Tagen:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --validity-days 3 \
  --renew-before-days 2 \
  --reissue-lead-days 2
```

## Automatische Laufzeit-Anpassung

`certbro` folgt dem CA/B-Forum-Zeitplan für Zertifikatslaufzeiten, nutzt dabei aber einen Sicherheitspuffer von einem Tag. Das heißt: `certbro` beginnt einen Tag vor dem offiziellen Stichtag mit dem jeweils niedrigeren Limit.

Offizielle maximale CA/B-Forum-Laufzeiten:

- ab `2026-03-15`: `200` Tage
- ab `2027-03-15`: `100` Tage
- ab `2029-03-15`: `47` Tage

Die schedule-aware Defaults in `certbro`:

- ab `2026-03-14`: `199` Tage
- ab `2027-03-14`: `99` Tage
- ab `2029-03-14`: `46` Tage

## Was mit älteren gespeicherten Werten passiert

Wenn ein verwaltetes Zertifikat noch einen inzwischen zu hohen Wert gespeichert hat, passt `certbro renew` ihn vor der Bestellung automatisch an.

Beispiel:

- für `example.com` sind noch `199` Tage gespeichert
- das Renewal läuft am oder nach `2027-03-14`
- die nächste effektive Bestellung verwendet `99` Tage
- der gespeicherte `validity_days`-Wert wird während der Renewal-Verarbeitung aktualisiert, sobald der Managed State fortgeschrieben wird

Diese automatische Anpassung gilt für gespeicherte Werte während der Renewal-Verarbeitung. Explizite CLI-Eingaben bleiben weiterhin strikt:

- `certbro issue --validity-days ...` wird sofort validiert
- `certbro update --validity-days ...` wird sofort validiert
- `certbro renew --validity-days ...` wird sofort validiert
- dieselben Timing-Regeln gelten außerdem für `certbro issue-pair` und `certbro import`

So bleiben bestehende verwaltete Zertifikate auch bei künftigen Schedule-Änderungen lauffähig, während bewusst gesetzte ungültige neue Werte weiterhin sauber abgelehnt werden.
