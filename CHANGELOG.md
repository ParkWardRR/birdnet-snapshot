# Changelog

All notable changes to this project. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/).

## [0.5.0] - 2026-10-27

### Added

- Field Notes shows when it was last updated: a chip on the cover ("Updated Oct 27, 7:29 am · 1 min ago", live while the tab is open, amber after 3 hours) and the same time in the tab title. The PDF prints the time without the "ago".
- A favicon: a songbird on a branch in the report's forest, cream and gold, as SVG, ICO (16, 32, 48) and an Apple touch icon. `report/web/scripts/make-icons.sh` rebuilds the raster sizes from `static/favicon.svg`.

### Removed

- The SvelteKit placeholder icon that the page layout still linked.

## [0.4.0] - 2026-10-26

### Added

- Field Notes **daily handoff**, three charts drawn in the page with no chart library: *Who sings when* (a ridgeline of each species' detections as a share of its own, earliest singer first), *Acoustic succession* (a streamgraph of detections per hour for the five most-heard species) and *Dawn chorus onset* (small panels of minutes after civil dawn, per morning singer and day). Hover or arrow keys give exact values; each chart has a table twin; the PDF prints them. Species keep one colour across the charts, from a palette validated for colour-blind separation.
- `-dawn` (default on) for the onset panels. They need BirdNET-Go's `dawn-onset` endpoint (older versions just get no panels) and two usable days. Only minutes from civil dawn are copied, never the dawn time or coordinates.

### Changed

- Section numerals follow the sections that are present; the heatmap is titled "Hour by hour".

## [0.3.0] - 2026-10-25

### Added

- Field Notes **Listen** section: the 3 best recordings of the period (clips several models agreed on first, then confidence) and the 3 latest, one per species in each list. Each is a spectrogram that plays from where you tap; one clip plays at a time; the PDF prints the spectrograms. `-clips` (default 3, 0 = none) and `-clip-min` (default 0.5). `-skip-audio`, `-no-audio`, BirdNET-Go false-positive marks and unknown sources keep a clip out.
- `deploy/systemd/birdnet-report.{service,timer}` (hourly) and `deploy/report.env.example`.
- `-forbid` now also checks the report's `data/report.json`.

### Fixed

- `-forbid` scanned binary files, so random bytes in a spectrogram PNG matched `172.16.[0-9]+.[0-9]+` and failed every snapshot build for 13 hours until fixed. Only text files are scanned now. The env examples single-quote patterns, since systemd drops unquoted backslashes (`\.` became `.`).
- README install command pointed at the 0.1.0 tarball under `latest/`, which 404s since 0.2.0.

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
