# Renewals und Ersatz

English version: [../en/renewals-and-replacement.md](../en/renewals-and-replacement.md)

## Reguläre Renewals

Alle verwalteten Zertifikate erneuern:

```sh
sudo certbro --state-file /etc/certbro/state.json renew
```

Ein einzelnes verwaltetes Zertifikat erneuern:

```sh
sudo certbro --state-file /etc/certbro/state.json renew --name example-com
```

Wenn Zertifikatsverzeichnisse unter einem gemeinsamen Root liegen, kann `certbro` sie automatisch finden:

```sh
sudo certbro --state-file /etc/certbro/state.json --certificates-dir /etc/certbro renew
```

Wenn eine Bestellung beim Erreichen des Wait-Timeouts noch `pending` ist, kann später einfach `certbro renew` erneut ausgeführt werden. `certbro` beobachtet dann denselben offenen Request weiter und startet keine doppelte Bestellung.

## Renewal vs. Reissue

`certbro` wählt automatisch den passenden regfish-API-Flow:

- Renewal-Order: für den Regelfall, in dem ein neues Zertifikat für ein bestehendes Zertifikat bestellt werden soll und Restlaufzeit angerechnet werden kann
- Reissue: wenn die Vertragslaufzeit deutlich über die Laufzeit des aktuell ausgestellten Zertifikats hinausgeht und der Vertrag Reissues unterstützt

In beiden Fällen rotiert `certbro` das Schlüsselmaterial und verwendet die gespeicherten Einstellungen aus `certbro.json`.

## Erzwungenes Renewal

Sofortige Renewal-Prüfung erzwingen:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force
```

Standardmäßig gibt `certbro renew` Fortschrittsmeldungen aus. Für ruhige Automatisierung kann `--quiet` genutzt werden.

## Einmaliger Laufzeit-Override

Wenn für einen erzwungenen Renewal-Lauf oder eine neue Renewal-Order eine andere Laufzeit angefordert werden soll, kann `--validity-days` gesetzt werden:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force \
  --validity-days 3
```

`--validity-days` gilt in diesem Lauf für Renewal-Orders und neue Orders. Für Reissues gilt der Wert nicht.

Den gespeicherten Default für künftige Renewal-Orders ändern:

```sh
sudo certbro --state-file /etc/certbro/state.json update \
  --name example-com \
  --validity-days 90
```

Wenn gespeicherte `validity_days` später über dem aktiven CA/B-Forum-Limit liegen, bricht `certbro` vor der Bestellung sauber ab, damit die Laufzeit explizit angepasst werden kann.

Die aktiven CA/B-Forum-Limits sind:

- vor `2026-03-15`: maximal `398` Tage
- ab `2026-03-15`: maximal `200` Tage
- ab `2027-03-15`: maximal `100` Tage
- ab `2029-03-15`: maximal `47` Tage

Die dazugehörigen schedule-aware Defaults in `certbro` sind `397`, `199`, `99` und `46` Tage.

## Aktives Zertifikat schnell ersetzen

Wenn das aktuell deployte Zertifikat sofort ersetzt werden soll, zum Beispiel mit anderer gewünschter Laufzeit, ist der typische Ablauf:

1. Aktuelle `certificate_id` notieren.
2. Renewal mit den gewünschten Parametern erzwingen.
3. Prüfen, dass das neue Zertifikat deployt und aktiv ist.
4. Das vorherige Zertifikat bei Bedarf in der regfish UI oder über die TLS API widerrufen.

Beispiel:

```sh
sudo certbro --state-file /etc/certbro/state.json renew \
  --name example-com \
  --force \
  --validity-days 3
```

`certbro` hat aktuell noch keinen eigenen `revoke`-Befehl. Das Widerrufen des vorherigen Zertifikats erfolgt daher über die regfish Console oder die TLS API.
