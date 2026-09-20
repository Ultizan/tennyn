#!/usr/bin/env sh
# build-release.sh <tag> — cross-compile into dist/ and write SHA256SUMS.
set -eu
tag="${1:?tag}"
rm -rf dist && mkdir -p dist
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os="${target%/*}"; arch="${target#*/}"
  out="dist/tennyn_${os}_${arch}"; [ "$os" = windows ] && out="$out.exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=$tag" -o "$out" .
done
cp install.sh dist/install.sh
(cd dist && sha256sum tennyn_* > SHA256SUMS)
ls -l dist
