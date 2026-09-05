# Repository security and release gates

## Review before publication

As verified on 2026-09-06, main requires a PR, up-to-date quality and Darwin
build checks, and resolved conversations. Force pushes and deletion are blocked
for administrators too. There is one maintainer and zero required independent
approvals; CODEOWNERS requests review but does not create a two-person guarantee.

CI actions are pinned to full commits with credential persistence disabled.
Dependabot proposes weekly action updates; it does not merge them. Review the
upstream source of each replacement revision and preserve the required job
names when updating workflows. Private vulnerability reporting is enabled.

These are repository controls, not an independent security audit. Read back
the live settings before relying on them for a new release.

## Current release behavior

The [release workflow](../../.github/workflows/release.yml) runs on a pushed
`vMAJOR.MINOR.PATCH` tag, builds Darwin arm64/amd64 archives, adds SHA256 checksums
and publishes a GitHub release. The release job has `contents: write`; CI does
not. **Pushing a matching tag can publish a release.** This PR does not add an
environment-approval gate or change the existing tag trigger.

No binary release was published at the 2026-09-06 review baseline. A passing
cross-build does not prove Intel hardware support, downloaded-byte integrity,
installation success or signing/notarization.

## Maintainer checklist

1. Reconcile [#8](https://github.com/sraodev/oli/issues/8), supported macOS versions,
   exact source/commit/version and unresolved P0 acceptance. Use reviewed source.
2. Obtain explicit release authorization before creating/pushing any tag.
   A merge or documentation PR is not that authorization.
3. Validate the actual arm64 and Intel install → run → update → uninstall paths.
   Record missing hardware coverage rather than substituting a cross-build.
4. Decide and accurately document signing, notarization and provenance. Never
   instruct users to disable platform trust controls as a default workaround.
5. Verify downloaded archives against SHA256SUMS and embedded build identity.
   Checksums from the same origin detect corruption, not independent publisher
   authentication.
6. Publish verified installation instructions only after the required endpoints
   and assets work. Track channels separately under #8; do not invent a domain.
7. Check release notes and rollback instructions. Do not replace released bytes
   under an existing version; correct defects through a new reviewed version.

Do not run the release workflow merely to test this checklist. Use fixture
archives and a non-publishing test harness until publication is authorized.
