# Changelog

All notable changes to this project. Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/).

## [0.1.0] - 2026-10-24

First public release.

### Added

- `crawl`: records the real BirdNET-Go UI in headless Chrome as a guest (dashboard for `-days` days, all analytics pages, species, search, about, prioritized detection lists, phone layout, `-routes`), learns detail-page API templates and replays them for the newest `-max-clips` detections with clips and large spectrograms.
- Static site with a service worker that answers the UI's API calls from content-addressed recordings, honours `Range` for audio, answers spectrogram status checks, refuses writes (`403`) and returns `404` for anything not recorded.
- `deploy`: checksum-diff upload to a Bunny storage zone, entry files last, one generation of grace, `-dry`.
- `run` (with `-every` for long-running mode), `serve` (local preview with CDN routing), `check` (BirdNET-Go, Chrome, Bunny), `bunny-setup` (storage zone, pull zone, cache rules, hostname, DNS, certificate; `-delete -yes`), `version`.
- Privacy: guest view forced, admin/settings/stream endpoints never requested, writes blocked inside Chrome, `-skip-audio` / `-no-audio` (also drops preloaded clips), RTSP URL scrubbing, `noindex` + `robots.txt` unless `-allow-indexing`.
- Safety: `-min-entries` broken-crawl guard, `-max-shrink` / `-force` shrink guard, atomic local builds.
- Timezone auto-detection from BirdNET-Go detection timestamps (or `TZ`), with Chrome's timezone overridden to match; dashboard and clock pinned to the snapshot.
- Prometheus textfile metrics (`-metrics-file`).
- `404.html` and `_redirects` for GitHub Pages, Cloudflare Pages and Netlify.
- Container image with Chrome bundled (`ghcr.io/parkwardrr/birdnet-snapshot`, linux/amd64 + arm64), systemd, quadlet and compose examples.
- Unit tests, fake Bunny storage and account APIs, and an end-to-end test that crawls a fake BirdNET-Go with real Chrome and browses the result.

[0.1.0]: https://github.com/ParkWardRR/birdnet-snapshot/releases/tag/v0.1.0
