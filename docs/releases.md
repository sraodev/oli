# Release and install contract (#8)

Source builds remain available. The implementation prepares releases but does
not authorize creating a tag, publishing assets, configuring account settings,
or enrolling an Apple signing identity. Local snapshots are not releases.

## Artifact contract

Pinned release compiler: Go 1.25.5, `CGO_ENABLED=0`, `-trimpath`, fixed
architecture defaults, no local Go flags/experiments, no automatic toolchain
download during packaging. Architectures: Darwin arm64 and amd64. The source
must be clean at HEAD and match an existing annotated `vMAJOR.MINOR.PATCH` tag.
Build date is the source commit time, not wall-clock packaging time.

Each archive has exactly three regular files beneath its version/architecture
directory: `mac-cleanup-studio` (0755), `LICENSE` and `RELEASE.json` (0644).
Tar ownership, timestamp, order and gzip metadata are normalized. Provenance
records version, source commit, source date, Go version, architecture, snapshot
status and signing status. The binary embeds version/commit/date and an identity
marker exposed by `capabilities`. The verifier checks Mach-O CPU/type, Go build
settings, embedded identity, member paths/modes, gzip integrity and checksums.
Archive expansion is bounded to 128 MiB, with 64 MiB maximum per member.

`SHA256SUMS` covers both archives and `install.sh`. Checksums detect byte
corruption, not publisher authenticity; obtain the manifest and script from
the same reviewed, authorized project release. No checksum signature or Apple
Developer ID verification is claimed.

Local rehearsal (does not tag or publish):

```sh
mkdir -p dist
go run ./cmd/releasepack --version v0.0.0 --snapshot --out dist/rehearsal-1
go run ./cmd/releasepack --version v0.0.0 --snapshot --out dist/rehearsal-2
diff -r dist/rehearsal-1 dist/rehearsal-2
go run ./cmd/releasepack --version v0.0.0 --snapshot --verify dist/rehearsal-1
```

Each output directory must be new. A snapshot records the checkout HEAD but may
include uncommitted changes; its commit is **not** a claim that those bytes
equal committed source. Production verification rejects snapshot provenance.
Reproducibility is for the same source and compiler, not different Go releases.

## Authorized publication

1. Finish review, tests, platform acceptance and the signing decision. Commit
   source and separately obtain authorization for a named version/source SHA.
2. The owner configures an existing `release` GitHub environment with required
   reviewers and self-review prevention. Do not assume naming an environment
   adds protection. The workflow fails closed if it cannot read/verify the gate.
3. Create the approved annotated tag. A push does not publish anything.
4. Manually dispatch the workflow from `main` with that version, exact SHA and
   `PUBLISH`; complete the environment review. Tests precede two builds, byte
   comparison and verification.
5. The workflow creates a draft without clobbering existing releases, downloads
   its assets, compares them byte-for-byte to local output, and verifies them
   again before making the draft public. Any intervening failure leaves it
   unpublished (a draft may remain for owner inspection). There is no automatic
   rollback, replacement, tag deletion or retry of an existing release.

No workflow was dispatched by local testing. Publication-stage failure tests
use local fake `gh`/`go` commands: they test ordering/fail-closed behavior, not
GitHub credentials, permissions, uploads or account configuration.

See [GitHub environment protections](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments).

## Per-user install, update and uninstall

Once an authorized release exists, download `SHA256SUMS`, `install.sh` and the
archive for your native architecture. Verify the downloaded script's SHA-256
against its exact `install.sh` line **before executing it**. Review the script.
It makes no network calls and independently checks the archive checksum.

Replace `vX.Y.Z`, the architecture and `/Users/YOUR_NAME/.local/bin` with the
approved version and your absolute, private, user-owned directory:

```sh
shasum -a 256 install.sh
bash install.sh install vX.Y.Z mac-cleanup-studio-vX.Y.Z-darwin-arm64.tar.gz SHA256SUMS /Users/YOUR_NAME/.local/bin
/Users/YOUR_NAME/.local/bin/mac-cleanup-studio version
```

For update, repeat with the new version/archive/manifest and add `--replace`
after the bin directory. Existing files are not overwritten without that flag.
Validation and native `version` execution happen in a temporary staging directory
before the binary is renamed into place. There is no automatic updater or
rollback; retain a trusted previous archive if needed.

```sh
bash install.sh uninstall /Users/YOUR_NAME/.local/bin --yes
```

Uninstall removes only that owned, regular `mac-cleanup-studio` file; unrelated
files, directories, configuration and future recovery data are retained. It
does not run cleanup. The installer refuses sudo, symlinked paths/targets,
group/other-writable install directories, wrong architectures/checksums and
unconfirmed replacement. On macOS use the canonical `/private/...` form rather
than the `/var` symlink for temporary test directories. No PATH/profile edit,
daemon, privilege escalation or security-setting change is performed.

## Signing and platform gates

The current artifact contract is **no Developer ID signature; not notarized**.
Go's arm64 linker signature is not a trusted Apple developer identity. The
project does not have signing/notarization credentials configured by this work.
If signed distribution is required, provision that identity under separate
owner authorization and implement/verify that pipeline before publishing.
Do not bypass Gatekeeper or strip quarantine attributes.

[Go 1.25 requires macOS 12 or later](https://go.dev/doc/go1.25); this is a runtime
minimum, not a compatibility-test matrix. Local verification is macOS 26.5,
arm64. The amd64 binary is cross-built and inspected, not executed on Intel.
Fresh arm64 and Intel machines must independently verify downloaded bytes,
Gatekeeper behavior, install/version/read-only scan, update failure/success and
uninstall without collateral deletion. These acceptance runs remain open.

Apple explains [Developer ID distribution](https://developer.apple.com/support/developer-id/)
and [Gatekeeper/notarization](https://support.apple.com/en-me/102445).
