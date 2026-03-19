# Import Existing Certificates

German version: [../de/import-existing-certificates.md](../de/import-existing-certificates.md)

Certificates ordered through the regfish UI can be brought under `certbro` management by `certificate_id`.

## Import for Renewal Management

```sh
sudo certbro --state-file /etc/certbro/state.json import \
  --certificate-id 7K9QW3M2ZT8HJ \
  --name example-com \
  --output-dir /etc/certbro/example.com
```

The imported certificate joins the normal `certbro renew` workflow.

## Import and Deploy Immediately

If the matching private key is already available locally, `certbro` can deploy the currently issued certificate immediately:

```sh
sudo certbro --state-file /etc/certbro/state.json import \
  --certificate-id 7K9QW3M2ZT8HJ \
  --name example-com \
  --output-dir /etc/certbro/example.com \
  --private-key-file /etc/ssl/private/example.com.key \
  --webserver nginx
```

## Notes

- Import currently supports issued certificates, not pending orders
- If a private key is supplied, `certbro` can deploy the current certificate immediately
- Future renewals and reissues use the locally managed settings in `certbro.json`
