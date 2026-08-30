# Security policy

## Supported versions

Until the first stable release, only the current `main` branch is in scope for
security fixes. After releases begin, this policy will identify the supported
release line explicitly.

## Reporting a vulnerability

Please do not open a public issue for a vulnerability that could cause
unintended deletion, escape the user-scoped path boundary, expose dashboard
authorization, bind the dashboard beyond loopback, or disclose scanned paths.

If the repository's **Security** tab offers **Report a vulnerability**, use it
to create a private report. Otherwise, contact the maintainer through the
private contact method listed on the
[`sraodev` GitHub profile](https://github.com/sraodev) and ask for a secure
reporting channel without including exploit details in the first message. If
neither option is available, open a public issue that asks only for a private
contact channel and contains no vulnerability details.

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
