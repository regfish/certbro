# Renewals und Ersatz

English version: [../en/renewals-and-replacement.md](../en/renewals-and-replacement.md)

## Reguläre Renewals

Alle verwalteten Zertifikate erneuern:

```sh
sudo certbro renew
```

Ein einzelnes verwaltetes Zertifikat erneuern:

```sh
sudo certbro renew --name example-com
```

Wenn Zertifikatsverzeichnisse unter einem nicht-defaultigen Root liegen, kann `certbro` sie automatisch finden:

```sh
sudo certbro --certificates-dir /srv/certbro renew
```

Wenn eine Bestellung beim Erreichen des Wait-Timeouts noch `pending` ist, kann später einfach `certbro renew` erneut ausgeführt werden. `certbro` beobachtet dann denselben offenen Request weiter und startet keine doppelte Bestellung.

Dieselbe Resume-Logik gilt auch für gestufte OV-/Business-Bestellungen. Wenn die TLS API `action_required=true` meldet, startet `certbro renew` keine doppelte Bestellung und wartet lokal nicht weiter. Stattdessen zeigt es die gespeicherte oder aktuelle `completion_url`, behält den Pending-Zustand bei und setzt die Bestellung erst fort, nachdem der Console-Schritt abgeschlossen wurde.

Nach diesem Console-Schritt provisioniert `certbro renew` DCV, sobald die Validierungsrecords verfügbar sind. Wenn das Zertifikat danach noch nicht ausgestellt werden kann, weil die providerseitige OV-/Business-Validierung weiterläuft, erzwingt `certbro` keine sofortige Issuance. Stattdessen endet der Lauf sauber, und ein späterer `renew`-Lauf oder der stündliche Timer setzt denselben Pending-Vorgang fort.

Damit frisch ausgestellte Zertifikate nicht sofort wieder in den Renewal-Flow geraten, überspringt `certbro` außerdem Zertifikate, die jünger als `48` Stunden sind, solange `--force` nicht gesetzt ist.

## Renewal vs. Reissue

`certbro` wählt automatisch den passenden regfish-API-Flow:

- Renewal-Order: für den Regelfall, in dem ein neues Zertifikat für ein bestehendes Zertifikat bestellt wird und der Provider nach der Ausstellung zusätzliche Restlaufzeit ergänzen kann
- Reissue: wenn die Vertragslaufzeit deutlich über die Laufzeit des aktuell ausgestellten Zertifikats hinausgeht und der Vertrag Reissues unterstützt

In beiden Fällen rotiert `certbro` das Schlüsselmaterial und verwendet die gespeicherten Einstellungen aus `certbro.json`.

## Erzwungenes Renewal

Sofortige Renewal-Prüfung erzwingen:

```sh
sudo certbro renew \
  --name example-com \
  --force
```

Standardmäßig gibt `certbro renew` Fortschrittsmeldungen aus. Für ruhige Automatisierung kann `--quiet` genutzt werden.

Bei gestuften OV-/Business-Bestellungen bedeutet das konkret: Die Ausgabe wechselt sauber von „wartet noch“ zu „Bestellung in der regfish Console abschließen“, sobald die API `action_required=true` zurückgibt.

## Einmaliger Laufzeit-Override

Wenn für einen erzwungenen Renewal-Lauf oder eine neue Renewal-Order eine andere gekaufte Basislaufzeit verwendet werden soll, kann `--validity-days` gesetzt werden:

```sh
sudo certbro renew \
  --name example-com \
  --force \
  --validity-days 30
```

`--validity-days` gilt in diesem Lauf für Renewal-Orders und neue Orders. Für Reissues gilt der Wert nicht.

Bei Renewal-Orders bleibt `--validity-days` die gekaufte Basislaufzeit der Bestellung. Wenn der Provider das Renewal erfolgreich verknüpft, kann das ausgestellte Zertifikat eine längere effektive Laufzeit haben. `certbro` behandelt `valid_from` und `valid_until` als maßgebliche ausgestellte Laufzeit.

Die gekaufte Basislaufzeit muss dabei größer bleiben als die gespeicherten Werte für `renew_before_days` und `reissue_lead_days`. Für sehr kurz laufende Zertifikate sollten diese Lead-Tage zuerst abgesenkt werden.

Den gespeicherten Default für künftige Renewal-Orders ändern:

```sh
sudo certbro update \
  --name example-com \
  --validity-days 90
```

Wenn gespeicherte `validity_days` später über dem aktiven schedule-aware Limit liegen, verwendet `certbro renew` automatisch den aktuellen schedule-aware Default und speichert den angepassten Wert während der Renewal-Verarbeitung dauerhaft zurück.

Offizielle CA/B-Forum-Limits:

- vor `2026-03-15`: maximal `398` Tage
- ab `2026-03-15`: maximal `200` Tage
- ab `2027-03-15`: maximal `100` Tage
- ab `2029-03-15`: maximal `47` Tage

`certbro` beginnt aus Sicherheitsgründen jeweils einen Tag früher mit dem niedrigeren Default. Die dazugehörigen schedule-aware Defaults sind:

- vor `2026-03-14`: `397` Tage
- ab `2026-03-14`: `199` Tage
- ab `2027-03-14`: `99` Tage
- ab `2029-03-14`: `46` Tage

Für konkrete Beispiele siehe [Laufzeitverwaltung](validity-management.md).

## Aktives Zertifikat schnell ersetzen

Wenn das aktuell deployte Zertifikat sofort ersetzt werden soll, zum Beispiel mit anderer gewünschter Laufzeit, ist der typische Ablauf:

1. Aktuelle `certificate_id` notieren.
2. Renewal mit den gewünschten Parametern erzwingen.
3. Prüfen, dass das neue Zertifikat deployt und aktiv ist.
4. Das vorherige Zertifikat bei Bedarf in der regfish UI oder über die TLS API widerrufen.

Beispiel:

```sh
sudo certbro renew \
  --name example-com \
  --force \
  --validity-days 30
```

`certbro` hat aktuell noch keinen eigenen `revoke`-Befehl. Das Widerrufen des vorherigen Zertifikats erfolgt daher über die regfish Console oder die TLS API.
