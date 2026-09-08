#!/bin/bash
# Build the same archive contract locally and in the tagged release workflow.
set -euo pipefail
[[ $# == 2 ]] || { echo 'Usage: bash scripts/package-release.sh vMAJOR.MINOR.PATCH NEW_OUTPUT_DIRECTORY' >&2; exit 2; }
version=$1
output=$2
[[ $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Invalid release version' >&2; exit 1; }
[[ $output == /* && ! -e $output && ! -L $output ]] || { echo 'Output must be a new absolute directory' >&2; exit 1; }
commit=$(git rev-parse HEAD)
build_date=$(git show -s --format=%cI HEAD)
mkdir -m 700 -- "$output"
for arch in arm64 amd64; do
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date" \
    -o "$output/oli" ./cmd/oli
  tar -C "$output" -czf "$output/oli-$version-darwin-$arch.tar.gz" oli
  rm -- "$output/oli"
done
cp scripts/install.sh "$output/install.sh"
(
  cd "$output"
  shasum -a 256 "oli-$version-darwin-arm64.tar.gz" "oli-$version-darwin-amd64.tar.gz" install.sh > SHA256SUMS
)
printf 'Packaged %s from %s at %s\n' "$version" "$commit" "$output"
