#!/bin/sh
# Install an archive from a published metra release after SHA-256 verification.
set -eu
repo=${METRA_REPO:-raywcm/metra}
version=${METRA_VERSION:-latest}
prefix=${METRA_PREFIX:-"$HOME/.local"}
case "$(uname -s)" in Linux) os=linux;; Darwin) os=darwin;; *) echo 'metra supports Linux and macOS' >&2; exit 1;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo 'metra requires amd64 or arm64' >&2; exit 1;; esac
case "$version" in latest) base="https://github.com/$repo/releases/latest/download";; *) base="https://github.com/$repo/releases/download/v${version#v}";; esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
curl -fL --retry 3 "$base/checksums.txt" -o "$work/checksums.txt"
# Match an exact artifact basename; never evaluate release text as shell code.
archive=$(awk -v suffix="_${os}_${arch}.tar.gz" '$2 ~ /^metra_[A-Za-z0-9.+_-]+_(linux|darwin)_(amd64|arm64)\.tar\.gz$/ && substr($2,length($2)-length(suffix)+1)==suffix {print $2}' "$work/checksums.txt")
case "$archive" in ''|*'
'*) echo 'No unique matching release archive' >&2; exit 1;; esac
curl -fL --retry 3 "$base/$archive" -o "$work/$archive"
expected=$(awk -v name="$archive" '$2==name {print $1}' "$work/checksums.txt")
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$work/$archive" | awk '{print $1}'); else actual=$(shasum -a 256 "$work/$archive" | awk '{print $1}'); fi
[ "$actual" = "$expected" ] || { echo 'SHA-256 verification failed' >&2; exit 1; }
# Extract only the binary; do not trust archive paths for installation.
tar -xzf "$work/$archive" -C "$work" metra
[ -f "$work/metra" ] && [ ! -L "$work/metra" ] || { echo 'Invalid binary in archive' >&2; exit 1; }
mkdir -p "$prefix/bin"
staged=$(mktemp "$prefix/bin/.metra-install.XXXXXX")
trap 'rm -f "$staged"; rm -rf "$work"' EXIT HUP INT TERM
install -m 755 "$work/metra" "$staged"
mv -f "$staged" "$prefix/bin/metra"
printf 'Installed metra to %s/bin/metra\nAdd %s/bin to your PATH if needed.\n' "$prefix" "$prefix"
