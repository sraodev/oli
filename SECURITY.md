# Security policy

## Supported versions

Until the first stable release, only the current `main` branch is in scope for
security fixes. After releases begin, this policy will identify the supported
release line explicitly.

## Reporting a vulnerability

Please do not open a public issue for a vulnerability that could cause
unintended deletion, escape the user-scoped path boundary, expose dashboard
authorization, bind the dashboard beyond loopback, or disclose scanned paths.

Use [Report a vulnerability](https://github.com/sraodev/oli/security/advisories/new)
to create a private report. Private vulnerability reporting is enabled. If you
cannot use that form, email the maintainer's public contact,
[srao.dev@gmail.com](mailto:srao.dev@gmail.com), asking for a secure reporting
channel without sending exploit details or personal scan data initially.

Include, where possible:

- the affected commit or version and macOS version;
- whether the issue occurs during scan, dry run, dashboard use, or applied
  cleanup;
- the smallest reproducible path layout or request;
- expected versus observed behavior;
- impact and any known workaround.

Do not include personal filenames or other sensitive scan output unless it is
essential and has been redacted. Please allow maintainers time to reproduce and
prepare a fix before public disclosure. This project does not currently offer a
bug bounty.

## Repository safeguards

Main requires a pull request, up-to-date passing quality and Darwin build
checks, and resolved review conversations. Force-pushes and branch deletion
are blocked, including for admins. Only GitHub Actions can satisfy the required
checks. A single maintainer currently owns this repository, so independent
approval is not mandatory; this is not a two-person review guarantee.

Workflows use immutable action revisions and read-only permissions except for
the release job's scoped publishing token. See the
[maintainer security and release guide](docs/maintainers/releasing.md).
These controls reduce repository risks; they are not a security audit or a
guarantee that cleanup is recoverable.
