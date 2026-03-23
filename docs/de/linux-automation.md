# Linux-Automatisierung

English version: [../en/linux-automation.md](../en/linux-automation.md)

`certbro install` schreibt und aktiviert unter Linux einen `systemd`-Service und Timer für unbeaufsichtigte Renewals.

## Timer installieren

```sh
sudo certbro install
```

Der installierte Timer führt standardmäßig stündlich `certbro renew` aus.

## Zeitplan anpassen

```sh
sudo certbro install --on-calendar 'daily'
```

## Nur Unit-Dateien schreiben

```sh
sudo certbro install --skip-systemctl
```

## Betriebshinweise

- Offene `pending`-Orders werden bei späteren Renewal-Läufen automatisch fortgesetzt
- Überlappende `certbro renew`-Läufe werden durch eine Lock-Datei verhindert
- Die erzeugte Environment-Datei enthält API-Key, State-Datei, optionales Zertifikats-Root und optionale Kontaktmetadaten

## Timer prüfen

```sh
sudo systemctl status certbro.timer --no-pager
sudo systemctl list-timers certbro.timer --all
```

## Cron als Alternative

Cron bleibt eine gültige Alternative:

```cron
17 3 * * * /usr/local/bin/certbro renew
```
