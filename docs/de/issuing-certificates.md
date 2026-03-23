# Zertifikate bestellen

English version: [../en/issuing-certificates.md](../en/issuing-certificates.md)

## Basis-Workflow

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com
```

Wenn `--output-dir` fehlt, schreibt `certbro` nach `<certificates-dir>/<common-name>`, bei den Linux-Defaults also nach `/etc/certbro/example.com`.

Für DV-Produkte erzeugt `certbro issue` lokal einen frischen Private Key und CSR, legt die Bestellung über die regfish TLS API an, provisioniert die benötigten `dns-cname-token`-Validierungsrecords über die regfish DNS API, wartet auf die Ausstellung, lädt das Zertifikat herunter und deployt es auf stabile Pfade unter `live/`.

Für OV- oder Business-Produkte kann die TLS API stattdessen eine gestufte Bestellung mit `action_required=true` und einer `completion_url` unter `https://dash.regfish.de/my/certs/...` zurückgeben. In diesem Fall erzeugt `certbro issue` den Private Key und CSR weiterhin lokal, speichert den Pending-Zustand, gibt die Console-URL aus und endet erfolgreich, ohne blockierend auf die Ausstellung zu warten.

Wenn `--validity-days` nicht gesetzt ist, nutzt `certbro` einen datumsabhängigen Default gemäß aktuellem CA/B-Forum-Zeitplan.

## OV- und Business-Completion-Flow

Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite
```

Wenn bereits eine nutzbare Organisations-ID aus der regfish Console bekannt ist, kann sie direkt mitgegeben werden:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite \
  --org-id 42
```

Damit wird die Bestellung direkt mit der vorhandenen Organisation verknüpft. Ist diese Organisation bereits bestellbar, kann die TLS API ohne gestufte Completion-URL direkt weiterlaufen.

Wenn die TLS API mit `action_required=true` antwortet, gibt `certbro` unter anderem diese Felder aus:

- `certificate_id`
- `pending_reason`
- `pending_message`
- `completion_url`

Die `completion_url` in der regfish Console öffnen, die OV-/Business-Bestellung dort abschließen und danach erneut ausführen:

```sh
sudo certbro renew --name example-com
```

`certbro renew` setzt denselben offenen Vorgang fort und provisioniert DCV, sobald die Validierungsrecords verfügbar sind. Wenn das Zertifikat danach schon bereitsteht, lädt `certbro` es im selben Lauf herunter und deployt es. Wenn die providerseitige OV-/Business-Validierung noch läuft, beendet sich `certbro renew` sauber und setzt den Vorgang bei einem späteren Renewal-Lauf oder Timer-Zyklus fort.

## Laufzeit-Zeitplan

`certbro` folgt dem CA/B-Forum-Zeitplan für öffentlich vertrauenswürdige TLS-Zertifikate und beginnt aus Sicherheitsgründen jeweils einen Tag früher mit dem niedrigeren Default.

- Vor `2026-03-14` ausgestellte Zertifikate: maximal `398` Tage, Default `397`
- Ab `2026-03-14` und vor `2027-03-14`: maximal `200` Tage, Default `199`
- Ab `2027-03-14` und vor `2029-03-14`: maximal `100` Tage, Default `99`
- Ab `2029-03-14`: maximal `47` Tage, Default `46`

Wenn `--validity-days` gesetzt wird, validiert `certbro` den Wert vor der Bestellung gegen das aktuell gültige Limit.

Die gekaufte Basislaufzeit muss außerdem größer sein als `--renew-before-days` und `--reissue-lead-days`. So verhindert `certbro`, dass direkt nach der Ausstellung sofort wieder ein Renewal oder Reissue fällig wird.

## Subject Alternative Names

`--dns-name` für jede SAN wiederholen:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --dns-name api.example.com
```

Die SAN-Liste wird im lokalen Verwaltungszustand gespeichert und später für Renewals und Reissues wiederverwendet.

## Produktauswahl

Das gewünschte `--product` wird vor der Bestellung gegen den live geladenen Produktkatalog der regfish TLS API validiert.

Beispiel mit einem nicht-defaultigen Produkt:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SSL123
```

Wenn das Produkt nicht existiert, bricht `certbro` ab und listet die verfügbaren Produktbezeichner aus der API-Ausgabe.

## Key-Algorithmen

RSA-Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --rsa-bits 3072
```

RSA ist der Default-Keytyp. Für eine abweichende RSA-Schlüssellänge reicht deshalb `--rsa-bits`.

ECDSA-Beispiel:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --key-type ecdsa \
  --ecdsa-curve p384
```

`certbro` rotiert das Schlüsselmaterial bei jeder neuen Bestellung, Renewal-Order und jedem Reissue.

Wenn RSA- und ECDSA-Varianten parallel für denselben Hostnamen betrieben werden sollen, siehe [`issue-pair`](dual-certificates.md).

## Fortschrittsausgabe

Standardmäßig gibt `certbro issue` Statusmeldungen für Produktvalidierung, DCV-Provisioning, Warten auf Ausstellung und Deployment aus.

Bei gestuften OV-/Business-Bestellungen passt sich die Ausgabe entsprechend an: `certbro` meldet den technischen Start der Bestellung, zeigt die Console-URL an und wartet lokal nicht weiter, bis der Console-Schritt abgeschlossen wurde.

Für ruhige Automatisierung oder Skriptbetrieb kann `--quiet` gesetzt werden:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --quiet
```
