# Bestehende Zertifikate importieren

English version: [../en/import-existing-certificates.md](../en/import-existing-certificates.md)

Zertifikate, die über die regfish UI bestellt wurden, können per `certificate_id` unter `certbro`-Verwaltung übernommen werden.

## Import für Renewal-Management

```sh
sudo certbro import \
  --certificate-id 7K9QW3M2ZT8HJ \
  --name example-com
```

Wenn `--output-dir` fehlt, importiert `certbro` nach `<certificates-dir>/<common-name>`, bei den Linux-Defaults also nach `/etc/certbro/example.com`.

Das importierte Zertifikat nimmt danach am normalen `certbro renew`-Workflow teil.

## Sofort importieren und deployen

Wenn der passende Private Key lokal bereits vorhanden ist, kann `certbro` das aktuell ausgestellte Zertifikat sofort deployen:

```sh
sudo certbro import \
  --certificate-id 7K9QW3M2ZT8HJ \
  --name example-com \
  --private-key-file /etc/ssl/private/example.com.key \
  --webserver nginx
```

## Hinweise

- Der Import unterstützt derzeit ausgestellte Zertifikate, keine `pending`-Orders
- Wenn ein Private Key mitgegeben wird, kann `certbro` das aktuelle Zertifikat sofort deployen
- Für spätere Renewals und Reissues verwendet `certbro` die lokal gespeicherten Einstellungen in `certbro.json`
