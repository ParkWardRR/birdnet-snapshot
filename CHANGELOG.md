# Changelog

All notable changes to this project. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/).

## [0.2.0] - 2026-10-25

### Added

- `report`: **Field Notes**, an illustrated report page and PDF for the last `-days` days: cover with the most-heard bird, a written summary, a 24-hour dial, a species-by-hour heatmap, photo cards, new arrivals, night shift, early risers, least expected visitors, day by day, every species, photo credits. A SvelteKit app embedded in the binary; 900 px Avicommons photos (credited) with BirdNET-Go's as fallback; PDF printed by headless Chrome; `-deploy` publishes it with the same Bunny deploy.
- `bunny-setup -fallback` for sites whose SPA fallback is not `/ui/index.html`; SvelteKit's `/_app/immutable/*` is cached for a year.
- `tests/browsers`: `-ready` and `-sw` to check pages other than snapshots.

### Changed

- `deploy` publishes any site with an `index.html`, not only snapshots.
- `bunny-setup` prints a generic URL line.

## [0.1.0] - 2026-10-24

First public release.

### Added

- `crawl`: records the real BirdNET-Go UI in headless Chrome as a guest: dashboard for `-days` days, all analytics pages, species, search, about, prioritized detection lists, phone layout, `-routes`. Learns detail-page API templates per detection (`{id}`) and per species (`{sci}`) from two sampled pages and replays them for the newest `-max-clips` detections, with clips and large spectrograms.
- Asset closure: every chunk, stylesheet, sound, icon and manifest referenced by the recorded JS, CSS and HTML is recorded, so rare code paths work.
- Media retry: spectrograms and clips still being written during the crawl are fetched again at the end.
- Static site with a service worker that answers the UI's API calls from content-addressed recordings (preloaded at install), honours `Range` for audio, answers spectrogram status checks, returns `401` for protected endpoints like a real server does for guests, refuses writes (`403`) and returns `404` for anything not recorded.
- Audio through the CDN: byte-range requests go straight to the CDN for a real `206` (Firefox rejects partial responses a worker builds for media elements); `bunny-setup` sets `.flac`/`.ogg`/`.m4a` content types.
- Upgrade-safe: deploy records the replaced manifest in `snapshot.json`, and the service worker falls back to it, so pages still running the previous BirdNET-Go build keep working.
- Compact manifest: 80-bit content names and content types derived from the file extension.
- `deploy`: checksum-diff upload to a Bunny storage zone, entry files last, one generation of grace, `-dry`.
- `run` (with `-every`), `serve` (local preview with CDN routing), `check` (BirdNET-Go, Chrome, Bunny), `bunny-setup`, `version`.
- `bunny-setup`: storage zone with SPA fallback, pull zone, year-long caching of content-addressed paths only, security headers (HSTS, nosniff, frame-deny, no-referrer, Permissions-Policy, COOP/CORP, `frame-ancestors`), writes blocked at the edge, stale-while-revalidate, web manifest content type, hostname, Bunny DNS CNAME, free certificate, Force SSL; `-purge`, `-delete -yes`. Idempotent, and adopts equivalent existing rules.
- Privacy: guest view forced; admin/settings/stream endpoints never requested; writes blocked inside Chrome; coordinates removed from recorded data (`-location hide|coarse|keep`); `-skip-audio` / `-no-audio` withhold clips **and spectrograms** (also preloaded ones) and blank `clipName` so the UI shows no player; RTSP URL scrubbing; `-redact` patterns; `noindex` + `robots.txt` unless `-allow-indexing`.
- Content-Security-Policy meta tag with the shell's inline scripts pinned by SHA-256, plus `no-referrer`.
- Safety: `-forbid` patterns fail the build if found in any published file; `-min-entries` broken-crawl guard; `-max-shrink` / `-force` shrink guard; atomic local builds.
- Timezone auto-detection from BirdNET-Go detection timestamps (or `TZ`), with Chrome's timezone overridden to match; dashboard and clock pinned to the snapshot.
- Prometheus textfile metrics (`-metrics-file`), written atomically.
- `404.html` and `_redirects` for GitHub Pages, Cloudflare Pages and Netlify.
- Container image with Chrome bundled (`ghcr.io/parkwardrr/birdnet-snapshot`, linux/amd64 + linux/arm64, non-root), built without a daemon by `tools/mkimage`; Dockerfile for local builds; systemd, quadlet and compose examples.
- Tests: unit tests, fake Bunny storage and account APIs, an end-to-end test that crawls a fake BirdNET-Go with real Chrome and browses the result, and `tests/browsers`, a Playwright check in Chromium, Firefox and WebKit that also plays a recorded clip in each engine.
- CI and releases on a self-hosted Podman runner with a same-repository guard and a runner policy check; scheduled browser check of a live snapshot.

[0.2.0]: https://github.com/ParkWardRR/birdnet-snapshot/releases/tag/v0.2.0
[0.1.0]: https://github.com/ParkWardRR/birdnet-snapshot/releases/tag/v0.1.0
