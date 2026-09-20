#!/usr/bin/env sh
# install.sh [VERSION] — fetch the tennyn release binary for this OS/arch,
# verify its sha256, install it, and put it on PATH (CI-aware).
#   TENNYN_DOWNLOAD_URL  releases base, default https://github.com/Ultizan/tennyn/releases
#   TENNYN_INSTALL_DIR   target dir, default $RUNNER_TEMP, else $AGENT_TEMPDIRECTORY, else ~/.local/bin
set -eu
version="${1:-latest}"
base="${TENNYN_DOWNLOAD_URL:-https://github.com/Ultizan/tennyn/releases}"
case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
  linux*) os=linux ;;
  darwin*) os=darwin ;;
  mingw*|msys*|cygwin*|windows*) os=windows ;;
  *) echo "tennyn: unsupported OS $(uname -s)" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "tennyn: unsupported arch $(uname -m)" >&2; exit 2 ;;
esac
asset="tennyn_${os}_${arch}"
[ "$os" = windows ] && asset="$asset.exe"
if [ "$version" = latest ]; then url="$base/latest/download"; else url="$base/download/$version"; fi
dir="${TENNYN_INSTALL_DIR:-${RUNNER_TEMP:-${AGENT_TEMPDIRECTORY:-$HOME/.local/bin}}}"
mkdir -p "$dir"
tmp="$(mktemp -d)"
curl -fsSL "$url/$asset" -o "$tmp/$asset"
curl -fsSL "$url/SHA256SUMS" -o "$tmp/SHA256SUMS"
expected="$(grep " $asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)"
if command -v sha256sum >/dev/null 2>&1; then actual="$(sha256sum "$tmp/$asset" | cut -d' ' -f1)"
else actual="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"; fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || { echo "tennyn: checksum mismatch for $asset" >&2; exit 2; }
bin="$dir/tennyn"; [ "$os" = windows ] && bin="$bin.exe"
mv "$tmp/$asset" "$bin"; chmod +x "$bin"; rm -rf "$tmp"
[ -n "${GITHUB_PATH:-}" ] && echo "$dir" >> "$GITHUB_PATH"
[ -n "${TF_BUILD:-}" ] && echo "##vso[task.prependpath]$dir"
echo "$bin"
