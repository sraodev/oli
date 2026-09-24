#!/bin/bash
# Verify the exact asset contract before a release is drafted or published.
set -euo pipefail

[[ $# == 2 ]] || { echo 'Usage: bash scripts/verify-release.sh vMAJOR.MINOR.PATCH ASSET_DIRECTORY' >&2; exit 2; }
version=$1
assets=$2
[[ $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Invalid release version' >&2; exit 1; }
[[ -d $assets && ! -L $assets ]] || { echo 'Asset directory is missing or symlinked' >&2; exit 1; }

arm="oli-$version-darwin-arm64.tar.gz"
intel="oli-$version-darwin-amd64.tar.gz"
for name in "$arm" "$intel" SHA256SUMS install.sh; do
  [[ -f $assets/$name && ! -L $assets/$name ]] || { echo "Missing or symlinked asset: $name" >&2; exit 1; }
done
for asset in "$assets"/*; do
  case ${asset##*/} in
    "$arm"|"$intel"|SHA256SUMS|install.sh) ;;
    *) echo "Unexpected release asset: ${asset##*/}" >&2; exit 1 ;;
  esac
done

expected_names=$(printf '%s\n' "$arm" "$intel" install.sh)
actual_names=$(awk '{print $2}' "$assets/SHA256SUMS")
[[ $actual_names == "$expected_names" ]] || { echo 'SHA256SUMS has unexpected entries' >&2; exit 1; }
(
  cd "$assets"
  shasum -a 256 -c SHA256SUMS
)
cmp -s scripts/install.sh "$assets/install.sh" || { echo 'Release installer differs from tagged source' >&2; exit 1; }

temp_binary=$(mktemp "${TMPDIR:-/tmp}/oli-verify.XXXXXX")
trap 'rm -f -- "$temp_binary"' EXIT
for arch in arm64 amd64; do
  archive="$assets/oli-$version-darwin-$arch.tar.gz"
  machine=$arch
  [[ $arch == amd64 ]] && machine=x86_64
  [[ $(tar -tzf "$archive") == oli ]] || { echo "Unexpected archive members: $archive" >&2; exit 1; }
  [[ $(tar -tvzf "$archive") == -* ]] || { echo "Archive member is not regular: $archive" >&2; exit 1; }
  tar -xOzf "$archive" oli > "$temp_binary"
  binary_type=$(file -b "$temp_binary")
  case $binary_type in
    *Mach-O*"$machine"*) ;;
    *) echo "Unexpected binary architecture in $archive: $binary_type" >&2; exit 1 ;;
  esac
done
echo "Verified exact release assets for $version"
