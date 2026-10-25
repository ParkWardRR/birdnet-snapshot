#!/usr/bin/env bash
# Toolchain for CI on the self-hosted runner (Ubuntu container, runs as root):
# Go, Node, the Playwright driver for playwright-go, and Chromium, Firefox and
# WebKit with their system libraries. Downloads are cached in the runner's
# persistent tool cache, so only the first run pays for them.
#
# The playwright-go driver is assembled from npm (playwright-core + Node):
# the prebuilt driver zips it would download are no longer published.
set -euo pipefail

GO_MINOR="$(awk '/^go /{split($2,v,"."); print v[1]"."v[2]}' go.mod)"
NODE_VERSION="22.23.4"   # SvelteKit 3 needs >= 22.17
PW_VERSION="1.50.1"   # must match tests/browsers' playwright-go (v0.5001.0)
cache="${RUNNER_TOOL_CACHE:-$HOME/.cache/ci-tools}"
mkdir -p "$cache"
export_env() { echo "$1=$2" >> "${GITHUB_ENV:-/dev/null}"; export "$1=$2"; }
add_path() { echo "$1" >> "${GITHUB_PATH:-/dev/null}"; export PATH="$1:$PATH"; }

# Go: newest patch release of the go.mod minor version.
go_version="$(curl -fsSL 'https://go.dev/dl/?mode=json' | jq -r --arg m "go$GO_MINOR" '[.[] | select(.version | startswith($m + ".") or . == $m)][0].version')"
[ "$go_version" != "null" ] || go_version="$(curl -fsSL 'https://go.dev/dl/?mode=json' | jq -r '.[0].version')"
if [ ! -x "$cache/$go_version/go/bin/go" ]; then
  mkdir -p "$cache/$go_version"
  curl -fsSL "https://go.dev/dl/$go_version.linux-amd64.tar.gz" | tar -xz -C "$cache/$go_version"
fi
add_path "$cache/$go_version/go/bin"
export_env GOPATH "$cache/gopath"
export_env GOMODCACHE "$cache/gopath/pkg/mod"
export_env GOCACHE "$cache/gocache"

# Node for the Playwright driver.
node_dir="$cache/node-v$NODE_VERSION-linux-x64"
if [ ! -x "$node_dir/bin/node" ]; then
  curl -fsSL "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-linux-x64.tar.xz" | tar -xJ -C "$cache"
fi
# npm runs "env node": put this Node ahead of the runner image's old one.
export PATH="$node_dir/bin:$PATH"
add_path "$node_dir/bin" # the report page is built with npm in CI

# playwright-go driver: <dir>/package/cli.js run by PLAYWRIGHT_NODEJS_PATH.
driver="$cache/ms-playwright-go/$PW_VERSION"
if [ ! -f "$driver/package/cli.js" ]; then
  mkdir -p "$driver"
  tgz="$(cd "$driver" && "$node_dir/bin/npm" pack --silent "playwright-core@$PW_VERSION")"
  tar -xzf "$driver/$tgz" -C "$driver" && rm -f "$driver/$tgz"
fi
export_env PLAYWRIGHT_DRIVER_PATH "$driver"
export_env PLAYWRIGHT_NODEJS_PATH "$node_dir/bin/node"
export_env PLAYWRIGHT_BROWSERS_PATH "$cache/ms-playwright"

# Browsers, plus their system libraries once per runner container.
"$node_dir/bin/node" "$driver/package/cli.js" install chromium firefox webkit
if [ ! -f /var/lib/birdnet-snapshot-ci-deps ]; then
  "$node_dir/bin/node" "$driver/package/cli.js" install-deps chromium firefox webkit
  touch /var/lib/birdnet-snapshot-ci-deps
fi

# The crawler and the chromedp end-to-end test use Playwright's Chromium.
chrome="$(ls -d "$PLAYWRIGHT_BROWSERS_PATH"/chromium-*/chrome-linux/chrome | sort -V | tail -1)"
export_env CHROME_PATH "$chrome"
export_env CHROME_NO_SANDBOX true

go version
"$chrome" --version
