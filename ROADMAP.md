# Roadmap

## What ships today (v0.2.0)

One Go binary (or a container with Chrome bundled) that turns a live BirdNET-Go UI into a read-only static site.

| Area | Shipped |
|---|---|
| Recording | Real UI in headless Chrome as a guest; dashboard ×N days, all analytics pages, species, search, about; detection lists by priority (species → hourly → species-hour); detail-page API templates learned and replayed for every detection; phone-layout pass; extra routes |
| Media | Clips and large spectrograms for the newest N detections; missed spectrograms/clips recovered after the crawl; spectrogram status answered from the recording |
| Privacy | Guest view forced; admin/settings/stream endpoints never requested; writes blocked in Chrome; coordinates removed; `-skip-audio` / `-no-audio`, including preloaded clips; RTSP scrubbing; `-redact`; noindex by default |
| Hardening | CSP with hashed inline scripts; edge security headers and write blocking via `bunny-setup`; `-forbid` leak guard |
| Safety | Broken-crawl guard (`-min-entries`), shrink guard (`-max-shrink`, `-force`), atomic local build, entry files uploaded last, one generation of grace |
| Publishing | Bunny storage checksum-diff deploy; `bunny-setup` (zones, cache rules, hostname, DNS, certificate, teardown); `404.html` + `_redirects` for other hosts; `serve` preview |
| Time | Timezone auto-detected from BirdNET-Go; dashboard pinned to the snapshot day; clock pinned to the snapshot time |
| Reporting | `report`: Field Notes page + PDF with Avicommons photos, written highlights, 24-hour dial, heatmap, species cards |
| Ops | `check`, Prometheus textfile metrics, `run -every`, systemd / quadlet / compose examples, daemonless multi-arch image, CI on a self-hosted runner with Chrome end-to-end and Chromium/Firefox/WebKit checks |

---

## What's next

| Priority | Item | Status |
|---|---|---|
| **Now** | `webserver.basepath` / reverse-proxy prefix support (crawl under `/birdnet/`, publish at `/`) | Planned |
| **Now** | Analytics period presets: also record 30-day and 1-year views of each analytics page | Planned |
| **Now** | Paginated detection lists: record pages 2–N of the busiest lists within the budget | Planned |
| Soon | Other targets: S3/R2/B2 (S3 API), Cloudflare Pages direct upload, plain `rsync`/directory deploy | Planned |
| Soon | Incremental crawl: reuse unchanged detail/clip responses from the previous snapshot instead of re-fetching | Planned |
| Soon | Locales: record the visitor-facing message bundles for every language BirdNET-Go ships | Planned |
| Soon | Smaller media: transcode spectrograms to WebP/AVIF and clips to Opus at build time | Idea |
| Soon | Source labels: rename audio sources in published data (e.g. "Doorbell" → "Front") | Idea |
| Soon | Search: pre-record searches for every detected species name so the search box works offline | Idea |
| Soon | Field Notes: month and season editions, year-over-year comparisons once there is history | Idea |
| Later | Snapshot history: keep dated snapshots side by side (`/2026-10-24/`) with a picker | Idea |
| Later | Optional password gate via Bunny token authentication | Idea |
| Later | BirdNET-Pi / BirdNET-PiPy support | Idea |

## Non-goals

- **Live data.** No proxying, tunnelling or streaming from the server. If you need live, BirdNET-Go's own auth plus a tunnel is the right tool.
- **Rewriting the UI.** The point is BirdNET-Go's own frontend; this project only records and replays it.

## Completed

| Version | Highlights |
|---|---|
| v0.2.0 | Field Notes: an illustrated report page and PDF |
| v0.1.0 | First public release (see [CHANGELOG](CHANGELOG.md)) |
