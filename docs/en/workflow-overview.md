# Workflow Overview

German version: [../de/workflow-overview.md](../de/workflow-overview.md)

This guide explains how `certbro`, the regfish TLS API, the regfish Console, and the regfish DNS API work together for different certificate types.

## DV Issue Flow

Example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com
```

Typical flow:

1. `certbro issue` generates the private key and CSR locally.
2. It submits the order through `POST /tls/certificate`.
3. For DV products, the TLS API usually creates the provider order immediately and returns `validation.dns_records` in the same response.
4. `certbro` provisions those `dns-cname-token` records through the regfish DNS API.
5. `certbro issue` waits for issuance, downloads the certificate, deploys it, and reloads the configured webserver.

In this flow, DNS validation is handled during the initial `issue` run.

## Staged OV and Business Flow

Example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite
```

Typical staged flow:

1. `certbro issue` still generates the private key and CSR locally and submits the order intent through the TLS API.
2. If organization or completion metadata is still missing, the TLS API returns a staged local resource with `action_required=true` and a `completion_url` under `/my/certs/...`.
3. `certbro` stores the pending key material and order metadata locally and exits successfully.
4. No DNS validation records are created yet, because the staged resource does not yet expose `validation.dns_records`.
5. You open the `completion_url` in the regfish Console and complete the OV/business order there.
6. A later `certbro renew` run loads the same certificate resource again through `GET /tls/certificate/{certificate_id}`.
7. As soon as the TLS API returns `action_required=false` and a `validation` block with `dns_records`, `certbro renew` provisions the CNAMEs through the regfish DNS API.
8. If the certificate is already ready afterwards, `certbro renew` downloads it, deploys it, and clears the pending state in the same run.
9. If provider-side OV/business validation is still pending, `certbro renew` returns cleanly after the DCV step and continues on a later renewal run or timer cycle.

Important difference from DV:

- The Console does not create the DCV CNAMEs.
- `certbro` still creates them.
- The only change is timing: for staged OV/business orders, the CNAMEs are created later, during `renew`, after the Console step has been completed and the TLS API actually exposes the validation data.
- Once those CNAMEs are in place, `certbro` no longer assumes that issuance must happen immediately. OV/business approval can continue asynchronously.

## OV and Business with an Existing Organization

Example:

```sh
sudo certbro issue \
  --name example-com \
  --common-name example.com \
  --product SecureSite \
  --org-id hdl_ABCDEFGHJKLMN
```

This changes the flow:

1. `certbro` still creates the private key and CSR locally.
2. The order is pre-linked to the existing public TLS organization id (`org_id`, for example `hdl_ABCDEFGHJKLMN`).
3. If that organization is already usable for ordering, the TLS API can continue directly without the staged `completion_url` detour.
4. In that case, the flow moves closer to DV behavior: validation records can appear immediately in the initial `issue` response, and `certbro issue` can provision DNS in the same run.
5. If the organization is incomplete or unusable, the TLS API can still fall back to the staged OV/business flow above.

Important for both API and CLI consumers:

- `POST /tls/certificate` accepts optional `org_id`.
- `POST /tls/certificate/{certificate_id}/complete` requires `org_id`.
- The response fields `organization_id` and `organization.id` use the same public string-based TLS organization id.
- Older numeric examples are no longer authoritative for this flow.

## What `renew` Does for Pending Orders

`certbro renew` is the only resume and finalize mechanism for pending orders.

It behaves like this:

1. If the certificate still reports `action_required=true`, `renew` does not create a duplicate order and does not keep waiting locally. It only shows the stored or current `completion_url`.
2. If `action_required=false` and validation records are available, `renew` provisions DNS and continues.
3. If the certificate is already issued, `renew` downloads and deploys it.

For staged OV/business orders, step 2 can also end as "DCV provisioned, provider-side validation still pending". That is a clean intermediate state, not an error.

The same command is also used by the hourly `systemd` timer. There is no separate OV-only timer.

## When DCV Records Appear

For staged OV/business orders, the validation records become available only after the Console completion has turned the staged resource into a real provider order and the TLS API can derive `validation` from that order.

That means:

- before Console completion: `action_required=true`, no usable DNS validation data
- after Console completion: `action_required=false`, eventually `validation.dns_records`
- once `validation.dns_records` are present: `certbro` can create the CNAMEs

`certbro renew` also checks for validation data during its polling loop. If the first poll after Console completion still has no DNS token, but a later poll in the same run does, `certbro` can provision the CNAMEs during that same `renew` run without waiting for the next timer cycle.

## Responsibility Split

- `certbro`: key generation, CSR generation, local state, DCV CNAME provisioning, download, deployment
- regfish TLS API: order state, provider linkage, validation instructions
- regfish Console: human completion step for staged OV/business orders
- regfish DNS API: actual DNS record creation and cleanup performed by `certbro`
