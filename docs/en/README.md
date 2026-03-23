# Documentation

German version: [../de/README.md](../de/README.md)

This directory contains the English operational guides for `certbro`.

## Start Here

- [Getting Started](getting-started.md): install `certbro`, configure a verified API key, and issue the first certificate
- [Linux Automation](linux-automation.md): install the hourly `systemd` renewal timer and operate it on Linux servers

## Certificate Workflows

- [Workflow Overview](workflow-overview.md): compare DV, staged OV/business, and pre-linked organization flows end to end
- [Issuing Certificates](issuing-certificates.md): single-domain, SAN, product selection, key algorithms, validity schedule, and quiet mode
- [Validity Management](validity-management.md): manual lifetime changes and automatic schedule-aware adjustment of stored values
- [Dual RSA and ECDSA Certificates](dual-certificates.md): operate parallel certificate variants for modern and legacy TLS clients
- [Renewals and Replacement](renewals-and-replacement.md): normal renewals, forced renewals, validity overrides, future CA/B Forum limits, and fast replacement
- [Import Existing Certificates](import-existing-certificates.md): bring certificates ordered in the regfish UI under `certbro` management

## Examples

- [Ubuntu + nginx Example](examples/ubuntu-nginx-certbro.md): full server walkthrough including DNS, `nginx`, `ufw`, `certbro`, and automatic renewals
- [Ubuntu + Apache Example](examples/ubuntu-apache-certbro.md): full server walkthrough including DNS, `apache2`, `ufw`, `certbro`, and automatic renewals
