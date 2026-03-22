# Zertifikate bestellen

English version: [../en/issuing-certificates.md](../en/issuing-certificates.md)

## Basis-Workflow

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

`certbro issue` erzeugt lokal einen frischen Private Key und CSR, legt die Bestellung über die regfish TLS API an, provisioniert die benötigten `dns-cname-token`-Validierungsrecords über die regfish DNS API, wartet auf die Ausstellung, lädt das Zertifikat herunter und deployt es auf stabile Pfade unter `live/`.

Wenn `--validity-days` nicht gesetzt ist, nutzt `certbro` einen datumsabhängigen Default gemäß aktuellem CA/B-Forum-Zeitplan.

## Laufzeit-Zeitplan

`certbro` folgt dem CA/B-Forum-Zeitplan für öffentlich vertrauenswürdige TLS-Zertifikate und beginnt aus Sicherheitsgründen jeweils einen Tag früher mit dem niedrigeren Default.

- Vor `2026-03-14` ausgestellte Zertifikate: maximal `398` Tage, Default `397`
- Ab `2026-03-14` und vor `2027-03-14`: maximal `200` Tage, Default `199`
- Ab `2027-03-14` und vor `2029-03-14`: maximal `100` Tage, Default `99`
- Ab `2029-03-14`: maximal `47` Tage, Default `46`

Wenn `--validity-days` gesetzt wird, validiert `certbro` den Wert vor der Bestellung gegen das aktuell gültige Limit.

Die gewünschte Laufzeit muss außerdem größer sein als `--renew-before-days` und `--reissue-lead-days`. So verhindert `certbro`, dass direkt nach der Ausstellung sofort wieder ein Renewal oder Reissue fällig wird.

## Subject Alternative Names

`--dns-name` für jede SAN wiederholen:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --dns-name www.example.com \
  --dns-name api.example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

Die SAN-Liste wird im lokalen Verwaltungszustand gespeichert und später für Renewals und Reissues wiederverwendet.

## Produktauswahl

Das gewünschte `--product` wird vor der Bestellung gegen den live geladenen Produktkatalog der regfish TLS API validiert.

Beispiel:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com
```

Wenn das Produkt nicht existiert, bricht `certbro` ab und listet die verfügbaren Produktbezeichner aus der API-Ausgabe.

## Key-Algorithmen

RSA-Beispiel:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --key-type rsa \
  --rsa-bits 3072 \
  --output-dir /etc/certbro/example.com
```

ECDSA-Beispiel:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --key-type ecdsa \
  --ecdsa-curve p384 \
  --output-dir /etc/certbro/example.com
```

`certbro` rotiert das Schlüsselmaterial bei jeder neuen Bestellung, Renewal-Order und jedem Reissue.

Wenn RSA- und ECDSA-Varianten parallel für denselben Hostnamen betrieben werden sollen, siehe [`issue-pair`](dual-certificates.md).

## Fortschrittsausgabe

Standardmäßig gibt `certbro issue` Statusmeldungen für Produktvalidierung, DCV-Provisioning, Warten auf Ausstellung und Deployment aus.

Für ruhige Automatisierung oder Skriptbetrieb kann `--quiet` gesetzt werden:

```sh
sudo certbro --state-file /etc/certbro/state.json issue \
  --name example-com \
  --common-name example.com \
  --product RapidSSL \
  --output-dir /etc/certbro/example.com \
  --quiet
```
