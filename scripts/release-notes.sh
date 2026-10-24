#!/usr/bin/env bash
# Prints the CHANGELOG section for a tag, plus install pointers.
set -euo pipefail
tag="${1:?usage: release-notes.sh <tag>}"
v="${tag#v}"
awk -v tag="$v" '
  $0 ~ "^## \\[?v?" tag "[] ]" { on=1; next }
  on && /^## / { exit }
  on && !/^\[[0-9]/ { print }
' CHANGELOG.md
cat <<NOTES

### Install

- Binaries for Linux, macOS and Windows are attached (check them against \`SHA256SUMS\`).
- Container with Chrome bundled (linux/amd64, linux/arm64): \`docker pull ghcr.io/parkwardrr/birdnet-snapshot:$v\`
NOTES
