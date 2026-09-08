#!/bin/bash
# Per-user release installer. Compatible with macOS Bash 3.2.
set -euo pipefail
umask 077

repository=https://github.com/sraodev/oli
version=latest
bin_dir=${HOME:?HOME is required}/.local/bin
mode=install
replace=false
yes=false
preview=false
archive=
checksums=
stage=

fail() { printf 'oli installer: %s\n' "$*" >&2; exit 1; }
usage() {
  printf '%s\n' \
    'Usage: bash install.sh [install|update|uninstall] [options]' \
    '  --version vMAJOR.MINOR.PATCH  Pin a release (default: latest stable)' \
    '  --bin-dir ABSOLUTE_PATH      Destination (default: ~/.local/bin)' \
    '  --replace                   Explicitly replace an existing oli file' \
    '  --yes                       Confirm uninstall of the selected oli file' \
    '  --dry-run                   Show the operation without network or writes' \
    '  --archive PATH --checksums PATH  Install a local release, with --version' \
    '  --help                      Show this help'
}
case ${1:-} in install|update|uninstall) mode=$1; shift ;; esac
while [[ $# -gt 0 ]]; do
  case $1 in
    --help|-h) usage; exit 0 ;;
    --version|--bin-dir|--archive|--checksums)
      [[ $# -ge 2 && -n $2 ]] || fail "$1 requires a value"
      case $1 in
        --version) version=$2 ;; --bin-dir) bin_dir=$2 ;;
        --archive) archive=$2 ;; --checksums) checksums=$2 ;;
      esac
      shift 2 ;;
    --replace) replace=true; shift ;;
    --yes) yes=true; shift ;;
    --dry-run) preview=true; shift ;;
    *) fail "unknown argument: $1" ;;
  esac
done
valid_version() { [[ $1 =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; }
[[ $version == latest ]] || valid_version "$version" || fail 'invalid release version'
[[ $(uname -s) == Darwin ]] || fail 'only macOS is supported'
[[ $(id -u) != 0 ]] || fail 'run as your own user, without sudo'
os_major=$(/usr/bin/sw_vers -productVersion); os_major=${os_major%%.*}
[[ $os_major =~ ^[0-9]+$ && $os_major -ge 12 ]] || fail 'macOS 12 or newer is required'
case $(uname -m) in
  arm64) arch=arm64 ;;
  x86_64)
    arch=amd64
    # Prefer the native binary even when launched from a translated terminal.
    if [[ $(/usr/sbin/sysctl -in sysctl.proc_translated 2>/dev/null || true) == 1 ]]; then arch=arm64; fi ;;
  *) fail 'unsupported architecture' ;;
esac
[[ $bin_dir == /* && $bin_dir != / && $bin_dir != */ && $bin_dir != *$'\n'* && $bin_dir != *$'\r'* && $bin_dir != *//* ]] || fail 'bin directory must be an absolute, non-root path without trailing slash'
check_path() {
  local cursor=$bin_dir
  while [[ $cursor != / ]]; do
    [[ ! -L $cursor ]] || fail 'symlinked installation paths are not supported'
    [[ ${cursor##*/} != . && ${cursor##*/} != .. ]] || fail 'dot path components are not supported'
    if [[ -e $cursor ]]; then
      [[ -d $cursor ]] || fail 'an installation path component is not a directory'
      local permissions
      permissions=$(/usr/bin/stat -f %Lp "$cursor")
      (( (8#$permissions & 0022) == 0 )) || fail 'installation ancestors must not be group/world writable'
    fi
    cursor=${cursor%/*}; [[ -n $cursor ]] || cursor=/
  done
}
check_path
target=$bin_dir/oli
[[ ! -L $target ]] || fail 'refusing a symlink at the destination'
if [[ -e $target ]]; then
  [[ -f $target && -O $target ]] || fail 'destination must be an owned regular file'
  if [[ $mode != uninstall ]]; then $replace || fail 'oli already exists; review its path and pass --replace'; fi
elif [[ $mode != install ]]; then
  fail 'no installation at the selected destination'
fi
if [[ $mode == uninstall ]]; then
  [[ -z $archive && -z $checksums && $version == latest && $replace == false ]] || fail 'uninstall accepts only --bin-dir, --yes, and --dry-run'
  $preview && { printf 'Would remove only %s\n' "$target"; exit 0; }
  $yes || fail 'uninstall requires --yes'
  /bin/rm -- "$target"
  printf 'Removed %s. Other files, settings and directories were retained.\n' "$target"
  exit 0
fi
if [[ -n $archive || -n $checksums ]]; then
  [[ -f $archive && -f $checksums && $version != latest ]] || fail 'local installation requires --archive, --checksums and an exact --version'
fi
$preview && { printf 'Would %s Oli %s for darwin/%s at %s. No downloads or writes.\n' "$mode" "$version" "$arch" "$target"; exit 0; }

download() {
  (
    # Also bound writes when an older curl receives no Content-Length header.
    ulimit -f "$(( ($3 + 1023) / 1024 ))"
    /usr/bin/curl --fail --silent --show-error --location --connect-timeout 15 --max-time 120 \
      --max-filesize "$3" --proto '=https' --proto-redir '=https' --tlsv1.2 --output "$2" "$1"
  )
  [[ $(/usr/bin/stat -f %z "$2") -le $3 ]] || fail 'download exceeds size limit'
}
if [[ $version == latest ]]; then
  resolved=$(/usr/bin/curl --fail --silent --show-error --head --location --connect-timeout 15 --max-time 30 \
    --proto '=https' --proto-redir '=https' --tlsv1.2 --output /dev/null --write-out '%{url_effective}' "$repository/releases/latest")
  [[ $resolved == "$repository/releases/tag/"* ]] || fail 'unexpected latest release redirect'
  version=${resolved#"$repository/releases/tag/"}
  valid_version "$version" || fail 'latest did not resolve to a stable version'
fi
asset=oli-$version-darwin-$arch.tar.gz
/bin/mkdir -p -- "$bin_dir"
check_path
[[ -O $bin_dir ]] || fail 'installation directory must be owned by this user'
stage=$(/usr/bin/mktemp -d "$bin_dir/.oli-install.XXXXXX")
cleanup() {
  # Only known files inside this invocation's private staging directory.
  /bin/rm -f -- "$stage/archive.tar.gz" "$stage/SHA256SUMS" "$stage/oli" "$stage/members" "$stage/types"
  /bin/rmdir -- "$stage"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [[ -z $archive ]]; then
  download "$repository/releases/download/$version/SHA256SUMS" "$stage/SHA256SUMS" 65536
  download "$repository/releases/download/$version/$asset" "$stage/archive.tar.gz" 67108864
else
  [[ $(/usr/bin/stat -f %z "$archive") -le 67108864 && $(/usr/bin/stat -f %z "$checksums") -le 65536 ]] || fail 'local release exceeds size limit'
  ( ulimit -f 65536; /bin/cp -- "$archive" "$stage/archive.tar.gz" )
  ( ulimit -f 64; /bin/cp -- "$checksums" "$stage/SHA256SUMS" )
fi
expected=$(/usr/bin/awk -v name="$asset" '$2 == name && NF == 2 { print $1 }' "$stage/SHA256SUMS")
[[ $expected =~ ^[0-9a-f]{64}$ ]] || fail 'manifest must contain exactly one checksum for the selected asset'
actual=$(/usr/bin/shasum -a 256 "$stage/archive.tar.gz"); actual=${actual%% *}
[[ $actual == "$expected" ]] || fail 'checksum mismatch; keep the old installation and report the release; never override the expected hash'

# Reject extra entries, directories, links and traversal. Never extract paths.
(
  ulimit -f 65536
  ulimit -t 30
  /usr/bin/tar -tzf "$stage/archive.tar.gz" > "$stage/members"
  /usr/bin/tar -tvzf "$stage/archive.tar.gz" > "$stage/types"
  [[ $(/bin/cat "$stage/members") == oli && $(/usr/bin/wc -l < "$stage/members") -eq 1 ]] || fail 'archive must contain only oli'
  [[ $(/usr/bin/cut -c1 "$stage/types") == - ]] || fail 'oli archive member must be a regular file'
  /usr/bin/tar -xOzf "$stage/archive.tar.gz" oli > "$stage/oli"
)
kind=$(/usr/bin/file -b "$stage/oli")
case "$arch:$kind" in arm64:Mach-O*arm64*|amd64:Mach-O*x86_64*) ;; *) fail 'binary architecture mismatch' ;; esac
/bin/chmod 0755 "$stage/oli"
identity=$("$stage/oli" version)
[[ $identity == "oli $version (commit "*", built "*")" ]] || fail 'embedded release version mismatch'
# Validate again immediately before the same-filesystem atomic replacement.
check_path
[[ ! -L $target ]] || fail 'destination became a symlink'
if [[ -e $target ]]; then
  [[ -f $target && -O $target && $replace == true ]] || fail 'destination changed; refusing replacement'
fi
/bin/mv -f -- "$stage/oli" "$target"
printf 'Installed %s\nPath: %s\n' "$identity" "$target"
printf '%s\n' 'No cleanup, services, shell profiles or security settings were changed.'
case :$PATH: in *":$bin_dir:"*) ;; *) printf 'Add this directory to PATH yourself: %s\nOr run: "%s" help\n' "$bin_dir" "$target" ;; esac
