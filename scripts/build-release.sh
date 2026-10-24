#!/usr/bin/env bash
# Cross-compiles release archives: build/<name>/ trees and dist/<name>.tar.gz
# (.zip for Windows) plus dist/SHA256SUMS.
#   scripts/build-release.sh v0.1.0 dist
set -euo pipefail
version="${1:?usage: build-release.sh <version> [dist-dir]}"
dist="${2:-dist}"
rm -rf build "$dist"
mkdir -p build "$dist"
for target in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64 windows/amd64; do
  os=${target%/*}; arch=${target#*/}
  name="birdnet-snapshot_${version#v}_${os}_${arch}"
  bin=birdnet-snapshot; [ "$os" = windows ] && bin=birdnet-snapshot.exe
  mkdir -p "build/$name"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "build/$name/$bin" .
  cp README.md LICENSE.md CHANGELOG.md "build/$name/"
  cp -r deploy "build/$name/"
  if [ "$os" = windows ]; then
    (cd build && zip -qr "../$dist/$name.zip" "$name")
  else
    tar -C build -czf "$dist/$name.tar.gz" "$name"
  fi
done
(cd "$dist" && sha256sum -- * > SHA256SUMS)
ls -l "$dist"
