# CLI installation and release lifecycle

Status: installer source and local fixture verification, **not a published
release channel yet**. No Homebrew, bun, PowerShell, mise or Linux installation
is advertised. Track [#44](https://github.com/sraodev/oli/issues/44) and the
[first release gates](https://github.com/sraodev/oli/issues/8).

## Build from a reviewed checkout now

Requires macOS and the Go version in `go.mod`:

```sh
git clone https://github.com/sraodev/oli.git
cd oli
make check
./bin/oli version
./bin/oli scan --profile safe
```

## Installer interface

The Bash 3.2-compatible installer uses only tools bundled with macOS. Installed
release binaries need no Go toolchain. The installer enforces macOS 12+ (the
[Go 1.25 minimum](https://go.dev/doc/go1.25#darwin)); minimum-version hardware
acceptance is still separate from a successful cross-build.

From a reviewed checkout, these commands need no release or network:

```sh
bash scripts/install.sh --help
bash scripts/install.sh --dry-run
bash scripts/install.sh --version v0.1.0 --dry-run
```

Once a release is **published and its assets verified**, download its
`install.sh` from the release page and review it before running it. A live curl
one-liner will be added here only after that public endpoint is tested. Do not
pipe an unreviewed script into a privileged shell.

The reviewed script accepts `install` (default), `update --replace`, and
`uninstall --yes`. `--version` pins a stable `vMAJOR.MINOR.PATCH`; otherwise
`latest` resolves once before downloading immutable-version asset URLs.
`--bin-dir` accepts an absolute directory, defaulting to `~/.local/bin`.
Uninstall is local and needs no network. `--dry-run` makes no downloads or writes.

After downloading and reviewing a published installer:

```sh
bash install.sh install --dry-run
bash install.sh install
"$HOME/.local/bin/oli" version
"$HOME/.local/bin/oli" help
bash install.sh update --replace
bash install.sh uninstall --dry-run
bash install.sh uninstall --yes
```

These lifecycle examples require the published installer; they are not a claim
that a release exists today. A custom `--bin-dir` must be supplied again for
update/uninstall. `--replace` explicitly authorizes replacing the exact `oli`
file there, including an unrelated executable with that name. Uninstall removes
only that explicitly selected owned regular file, not settings, data or the
installation directory. Keep a previous verified release if you need rollback:
install that exact version with `--replace`.

## Offline / manually downloaded assets

An archive contains exactly one regular member, `oli`. Release assets are named
`oli-VERSION-darwin-arm64.tar.gz` and `oli-VERSION-darwin-amd64.tar.gz`, with
`SHA256SUMS` and `install.sh`. Choose the native architecture, including arm64
on Apple Silicon when your terminal is translated.

After obtaining all files for the same reviewed release, substitute its exact
version below:

```sh
bash install.sh --version v0.1.0 \
  --archive ./oli-v0.1.0-darwin-arm64.tar.gz \
  --checksums ./SHA256SUMS
```

The installer checks the asset hash, archive membership/type, Mach-O
architecture, and embedded version **before** an atomic same-filesystem rename.
It refuses symlinked paths, unsafe writable ancestors and unconfirmed overwrite.
Failed validation retains the previous binary. Interrupted downloads leave no
partial binary at the destination; a new empty installation directory can
remain. A forced kill or power loss can leave the private staging directory.

Downloads use HTTPS-only redirects with connect/total timeouts and size limits.
Same-origin SHA-256 checks detect corruption but are **not independent publisher
authentication**. Never replace the expected hash to silence a mismatch. Stop,
retain the old binary, and report the failed asset. The downloaded installer
itself must be trusted/reviewed before execution. No Developer ID signing or
notarization claim is made; do not disable Gatekeeper to install Oli.

No `sudo`, shell-profile edits, hooks, services, cleanup, process killing,
package-manager update or macOS security-setting changes occur. Output is plain
text, also when `NO_COLOR` is set. If the destination is not in `PATH`, the
installer reports it; configure your own shell or use the absolute executable.

## Maintainer packaging and publication gate

From a reviewed checkout, use a new output directory:

```sh
bash scripts/package-release.sh v0.1.0 "$PWD/dist/release-candidate"
```

This is a local candidate, not a signed or published release. It embeds the
checkout's HEAD commit, so do not distribute a dirty-tree build as that commit.
Local candidates are never uploaded as replacement release assets.

The tag-triggered workflow builds archives from the tag, runs tests and native
installer lifecycle checks, then creates a **draft** release. Publication is a
separate maintainer decision after:

- exact tag/commit, manifest, archive members and version readback are verified;
- native Apple Silicon and Intel E2E pass (cross-compilation alone is not proof);
- supported macOS versions and unsigned/not-notarized delivery are disclosed;
- outstanding P0 safety/reporting gates, including #68, are reviewed;
- actual downloaded release bytes pass pinned and latest install/update checks;
- failure coverage gaps (network timeout/interruption, low disk, Rosetta and
  minimum macOS) are recorded rather than described as tested.

Keep #8 and #44 open until their complete acceptance boundaries are satisfied.
