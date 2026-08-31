#!/bin/bash
# Offline, per-user installation of a checksum-verified release archive.
set -euo pipefail
umask 077
fail() { echo "install: $*" >&2; exit 1; }
usage() {
  echo "Usage: bash install.sh install VERSION ARCHIVE SHA256SUMS BIN_DIR [--replace]" >&2
  echo "       bash install.sh uninstall BIN_DIR --yes" >&2
  exit 2
}
[[ $(uname -s) == Darwin ]] || fail "macOS is required"
[[ $(id -u) != 0 ]] || fail "run as your own user, without sudo"
[[ ${1:-} == install || ${1:-} == uninstall ]] || usage
mode=$1
if [[ $mode == install ]]; then
  [[ $# == 5 || ( $# == 6 && $6 == --replace ) ]] || usage
  version=$2 archive=$3 checksums=$4 bin_dir=$5
  [[ $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail "invalid version"
  major=$(sw_vers -productVersion); major=${major%%.*}
  [[ $major -ge 12 ]] || fail "this Go build requires macOS 12 or newer; see tested-platform notes"
else
  [[ $# == 3 && $3 == --yes ]] || usage
  bin_dir=$2
fi
[[ $bin_dir == /* && $bin_dir != / && $bin_dir != */ && $bin_dir != *$'\n'* ]] || fail "BIN_DIR must be an absolute directory path without a trailing slash"
cursor=$bin_dir
while [[ $cursor != / ]]; do
  [[ ! -L $cursor ]] || fail "symlinked installation directories are not supported"
  [[ ${cursor##*/} != . && ${cursor##*/} != .. ]] || fail "dot path components are not supported"
  cursor=${cursor%/*}; [[ -n $cursor ]] || cursor=/
done
target=$bin_dir/mac-cleanup-studio
if [[ $mode == uninstall ]]; then
  [[ -d $bin_dir && -O $bin_dir && -f $target && ! -L $target && -O $target ]] || fail "no owned regular installation at the selected path"
  rm -- "$target"
  echo "Removed $target. Other files and directories were retained."
  exit
fi
case $(uname -m) in arm64) arch=arm64 ;; x86_64) arch=amd64 ;; *) fail "unsupported architecture" ;; esac
bundle=mac-cleanup-studio-$version-darwin-$arch
expected=$(awk -v name="$bundle.tar.gz" '$2 == name { print $1 }' "$checksums")
[[ $expected =~ ^[0-9a-f]{64}$ ]] || fail "checksum manifest must contain exactly one checksum for this version/architecture"
actual=$(/usr/bin/shasum -a 256 "$archive"); actual=${actual%% *}
[[ $actual == "$expected" ]] || fail "archive checksum mismatch"
mkdir -p -- "$bin_dir"
[[ -O $bin_dir && ! -L $bin_dir ]] || fail "installation directory must be owned by this user"
permissions=$(/usr/bin/stat -f %Lp "$bin_dir")
(( (8#$permissions & 0022) == 0 )) || fail "installation directory must not be writable by group/others"
[[ ! -L $target ]] || fail "refusing to replace a symlink"
if [[ -e $target ]]; then
  [[ ${6:-} == --replace && -f $target && -O $target ]] || fail "installation exists; review it and explicitly pass --replace to update"
fi
stage=$(mktemp -d "$bin_dir/.mcs-install.XXXXXX")
cleanup() { rm -f -- "$stage/mac-cleanup-studio"; rmdir -- "$stage"; }
trap cleanup EXIT
trap 'exit 130' INT TERM
# Stream only the known member to a fresh regular file. Never extract archive paths.
/usr/bin/tar -xOzf "$archive" "$bundle/mac-cleanup-studio" > "$stage/mac-cleanup-studio"
kind=$(/usr/bin/file -b "$stage/mac-cleanup-studio")
case "$arch:$kind" in arm64:Mach-O*arm64*|amd64:Mach-O*x86_64*) ;; *) fail "archive does not contain the expected Mach-O architecture" ;; esac
chmod 0755 "$stage/mac-cleanup-studio"
identity=$("$stage/mac-cleanup-studio" version)
[[ $identity == "mac-cleanup-studio $version (commit "* ]] || fail "embedded version does not match the requested release"
# Rename within the same directory filesystem: a failed validation never touches
# an existing binary. --replace explicitly authorizes replacing this exact file.
mv -f -- "$stage/mac-cleanup-studio" "$target"
echo "Installed $identity at $target"
echo "No shell profile, daemon, cleanup setting, or macOS security setting was changed."
