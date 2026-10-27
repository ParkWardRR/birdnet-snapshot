#!/bin/sh
# Rebuild the raster icons from static/favicon.svg (the source of truth):
# favicon.ico (16/32/48), apple-touch-icon.png (180, square: iOS rounds it)
#. Needs Chrome (CHROME=/path to override) and ImageMagick.
set -e
cd "$(dirname "$0")/../static"
CHROME=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

render() { # svg size out
	cat > "$tmp/i.html" <<HTML
<!doctype html><html style="background:transparent"><body style="margin:0;background:transparent"><img src="file://$1" width="$2" height="$2" style="display:block"></body></html>
HTML
	"$CHROME" --headless=new --disable-gpu --hide-scrollbars --force-device-scale-factor=1 \
		--default-background-color=00000000 --window-size="$2,$2" --screenshot="$3" "file://$tmp/i.html" >/dev/null 2>&1
}

svg=$PWD/favicon.svg
for s in 16 32 48; do render "$svg" $s "$tmp/$s.png"; done
sed 's/ rx="14"//' favicon.svg > "$tmp/square.svg"
render "$tmp/square.svg" 180 apple-touch-icon.png
magick "$tmp/16.png" "$tmp/32.png" "$tmp/48.png" favicon.ico
ls -l favicon.svg favicon.ico apple-touch-icon.png
