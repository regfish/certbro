# Linux Automation

German version: [../de/linux-automation.md](../de/linux-automation.md)

`certbro install` writes and enables a `systemd` service and timer for unattended renewals on Linux.

## Install the Timer

```sh
sudo certbro --state-file /etc/certbro/state.json install \
  --certificates-dir /etc/certbro
```

The installed timer runs `certbro renew` hourly by default.

## Customize the Schedule

```sh
sudo certbro --state-file /etc/certbro/state.json install \
  --certificates-dir /etc/certbro \
  --on-calendar 'daily'
```

## Write Unit Files Only

```sh
sudo certbro --state-file /etc/certbro/state.json install \
  --certificates-dir /etc/certbro \
  --skip-systemctl
```

## Operational Notes

- Pending orders are resumed automatically on later renewal runs
- Overlapping `certbro renew` runs are prevented by a lock file
- The generated environment file includes the API key, state file path, optional certificates root, and optional contact metadata

## Check the Timer

```sh
sudo systemctl status certbro.timer --no-pager
sudo systemctl list-timers certbro.timer --all
```

## Cron Alternative

Cron remains a valid alternative:

```cron
17 3 * * * /usr/local/bin/certbro --state-file /etc/certbro/state.json renew
```
