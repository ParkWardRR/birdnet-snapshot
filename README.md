<div align="center">

# birdnet-snapshot

**Share your [BirdNET-Go](https://github.com/tphakala/birdnet-go) station with anyone, without exposing it.**

Records the real BirdNET-Go web UI in headless Chrome and publishes it as a read-only static site on a CDN.
Visitors get the dashboard, species, analytics, detections, spectrograms and audio clips they know.
Your server never sees a single visitor.

[![License: Blue Oak 1.0.0](https://img.shields.io/badge/License-Blue_Oak_1.0.0-0a7bbb.svg?style=flat-square)](LICENSE.md)
[![Release](https://img.shields.io/github/v/release/ParkWardRR/birdnet-snapshot?style=flat-square&color=success)](https://github.com/ParkWardRR/birdnet-snapshot/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/ParkWardRR/birdnet-snapshot/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/ParkWardRR/birdnet-snapshot/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/ParkWardRR/birdnet-snapshot?style=flat-square&logo=go&logoColor=white&color=00ADD8)](go.mod)
[![Container](https://img.shields.io/badge/ghcr.io-birdnet--snapshot-2496ED?style=flat-square&logo=docker&logoColor=white)](https://github.com/ParkWardRR/birdnet-snapshot/pkgs/container/birdnet-snapshot)

[![BirdNET-Go](https://img.shields.io/badge/for-BirdNET--Go-10b981?style=flat-square)](https://github.com/tphakala/birdnet-go)
[![Bunny CDN](https://img.shields.io/badge/deploys_to-Bunny_CDN-FF7854?style=flat-square)](https://bunny.net)
[![Any static host](https://img.shields.io/badge/or-any_static_host-6B7280?style=flat-square)](#other-static-hosts)
[![Platforms](https://img.shields.io/badge/platform-linux_%7C_macOS_%7C_windows-informational?style=flat-square)](#install)
[![Read-only](https://img.shields.io/badge/visitors-read--only-8B5CF6?style=flat-square)](#privacy-and-safety)
[![Chrome](https://img.shields.io/badge/headless-Chrome-4285F4?style=flat-square&logo=googlechrome&logoColor=white)](https://github.com/chromedp/chromedp)
[![Tested in](https://img.shields.io/badge/tested_in-Chromium_%7C_Firefox_%7C_WebKit-2EAD33?style=flat-square&logo=playwright&logoColor=white)](#testing)
[![CSP](https://img.shields.io/badge/CSP-hash--locked-1F2937?style=flat-square)](#privacy-and-safety)
[![Location](https://img.shields.io/badge/location-redacted-DC2626?style=flat-square)](#privacy-and-safety)
[![CI runner](https://img.shields.io/badge/CI-self--hosted_podman-0F766E?style=flat-square&logo=podman&logoColor=white)](#testing)

<br/>

<img src="docs/images/dashboard.jpg" alt="The BirdNET-Go dashboard served from a static snapshot, with the snapshot pill in the corner" width="860"/>

<sub>The real BirdNET-Go dashboard, served from Bunny CDN. The pill in the corner is the only addition.</sub>

</div>

---

## Why

BirdNET-Go is great to share, but its UI has admin controls, live streams and no read-only mode. Opening it to the internet (a tunnel, a port forward, Tailscale Funnel) puts your server and your settings one bug away from strangers.

`birdnet-snapshot` takes the other route: it **copies the UI out** instead of letting people in. Every hour (or whenever you like) it records what a guest would see and pushes that to a CDN. The result looks and behaves like BirdNET-Go because it *is* BirdNET-Go's own frontend; only the data comes from a recording.

## Features

| | |
|---|---|
| 🪞 **The real UI** | No reimplementation. BirdNET-Go's own Svelte frontend runs in the visitor's browser; a service worker answers its API calls from the recording |
| 📊 **Everything a guest sees** | Dashboard for the last 7 days, all analytics pages, species, search, about, up to 150 detection lists, detail pages with large spectrograms and audio clips, and the phone layout |
| 🔒 **Read-only and offline from your server** | Writes get `403`, live streams are stubbed, settings never load. The CDN serves everything; BirdNET-Go is never reached |
| 🕵️ **Privacy filters** | Station coordinates removed from every response (`-location hide`, or `coarse` for ~100 km), clips from sources that hear people skipped (`-skip-audio doorbell`) or no audio at all, RTSP URLs and their credentials scrubbed, your own `-redact` patterns, `noindex` by default |
| 🚨 **Leak guard** | `-forbid` patterns (your home domain, LAN range…) are checked against every published file; a match fails the build and nothing is uploaded |
| 🧱 **Hardened** | Strict Content-Security-Policy with the shell's inline scripts pinned by hash, no third-party requests. `bunny-setup` adds HSTS, nosniff, frame-deny, no-referrer, Permissions-Policy and COOP/CORP, and blocks writes at the edge |
| 🛡️ **Safe publishing** | A crawl that is clearly broken (server restarting, UI failing) is never published, and a snapshot that shrinks by more than half needs `-force` |
| ⚡ **Cheap incremental deploys** | Content-addressed files and a checksum diff: an hourly run uploads only what changed. Entry files are swapped last, so visitors never see a half-uploaded site |
| ☁️ **One-command Bunny setup** | `bunny-setup` creates the storage zone, pull zone, cache rules, security headers, custom hostname, DNS record and TLS certificate, and is safe to re-run |
| 🚀 **Fast** | App starts in 0.1–0.4 s on a first visit and ~0.15 s on repeat visits (measured in Chromium and WebKit): Brotli, immutable caching of content-addressed files, the manifest preloaded by the service worker, network work started before the DOM is parsed |
| 🌍 **Any static host** | Writes `404.html` and `_redirects`, so GitHub Pages, Cloudflare Pages, Netlify, nginx or Caddy work too |
| 🕐 **Time-correct** | Detects BirdNET-Go's timezone, pins the dashboard to the snapshot's day and the clock to the snapshot's time, so "today" and "5 minutes ago" stay true |
| 📈 **Monitoring** | Prometheus textfile metrics: last success, duration, pages, entries, clips, bytes, uploads |
| 🐳 **Batteries included** | Single static binary, or a container with Chrome bundled (amd64 + arm64). systemd, quadlet and compose examples |

<table>
<tr>
<td width="68%"><img src="docs/images/detail.jpg" alt="A detection detail page with the large spectrogram and audio player"/></td>
<td width="32%"><img src="docs/images/mobile.jpg" alt="The phone layout of the snapshot"/></td>
</tr>
<tr>
<td align="center"><sub>Detail page: species, taxonomy, weather, spectrogram and playable clip</sub></td>
<td align="center"><sub>The phone layout works too</sub></td>
</tr>
<tr>
<td colspan="2"><img src="docs/images/analytics.jpg" alt="The analytics summary page of the snapshot"/></td>
</tr>
<tr>
<td colspan="2" align="center"><sub>Analytics: totals, top species, time of day, trends</sub></td>
</tr>
</table>

## How it works

```mermaid
flowchart LR
    subgraph home["Your network"]
        B["BirdNET-Go<br/>:8080"]
        S["birdnet-snapshot<br/>+ headless Chrome"]
    end
    subgraph cdn["CDN (Bunny or any static host)"]
        F["static site<br/>UI shell · assets<br/>/_snap/ recorded responses<br/>sw.js"]
    end
    V["Visitor's browser"]

    S -- "1 · drives the guest UI,<br/>records every response" --> B
    S -- "2 · checksum-diff upload" --> F
    V -- "3 · loads the real UI" --> F
    V -. "4 · /api/ calls answered<br/>by the service worker" .-> F
```

1. **Crawl.** Chrome loads the UI as a guest (the crawler rewrites `app/config` so the UI thinks auth is on and nobody is logged in). It visits the seed pages, follows detection links by priority, and opens two detail pages of different species to learn which API calls a detail page makes, as `{id}` and `{sci}` templates. Those are replayed for every recorded detection, along with clips and spectrograms. Every chunk, stylesheet, sound and icon referenced by the recorded code is fetched too, so rare code paths (error screens, lazy routes) work. Media still being written during the crawl is retried at the end. Admin, settings, stream and notification endpoints are blocked, and so is every write except "render this spectrogram".
2. **Build.** Responses are scrubbed (coordinates, stream URLs, your `-redact` patterns) and checked against `-forbid`. Assets keep their paths. API responses go to `/_snap/<sha256>.<ext>`, indexed by a manifest that `snapshot.json` points to. The UI shell gets a Content-Security-Policy with its inline scripts pinned by hash, and a boot script that stubs `EventSource`, pins the clock, and starts the app once the service worker controls the page.
3. **Deploy.** Only files whose checksum changed are uploaded; `snapshot.json` goes last. Files from the previous snapshot are kept one more round so visitors mid-session don't break.
4. **Visit.** The service worker (preloaded with the manifest at install) maps each API call, minus cache-busting params, to its recorded response. It answers spectrogram status checks from what was recorded and returns `401` for protected endpoints, as a real server does for guests, so the UI stays quiet. Writes get `403`, and anything the crawl never saw gets `404`.

## Install

**Binary** (Linux, macOS, Windows; needs Chrome or Chromium installed, or a running one via `-chrome`):

```sh
# pick your platform from https://github.com/ParkWardRR/birdnet-snapshot/releases
curl -L https://github.com/ParkWardRR/birdnet-snapshot/releases/latest/download/birdnet-snapshot_0.1.0_linux_amd64.tar.gz | tar -xz
sudo install birdnet-snapshot_0.1.0_linux_amd64/birdnet-snapshot /usr/local/bin/
```

**Container** with Chrome bundled (amd64, arm64):

```sh
docker pull ghcr.io/parkwardrr/birdnet-snapshot:latest
```

**From source**: `go install github.com/ParkWardRR/birdnet-snapshot@latest`

## Quick start

**1. Check that everything is reachable.**

```sh
birdnet-snapshot check -birdnet http://127.0.0.1:8080
```
```
birdnet-snapshot v0.1.0 check
  ✓ birdnet    http://127.0.0.1:8080, BirdNET-Go 20261014
  ✓ chrome     Chrome/157.0.8103.15 (launched)
  - bunny      not configured (BUNNY_STORAGE_ZONE / BUNNY_STORAGE_KEY); crawl and serve still work
```

**2. Try it locally.**

```sh
birdnet-snapshot crawl -birdnet http://127.0.0.1:8080 -out site -skip-audio doorbell
birdnet-snapshot serve -out site          # http://127.0.0.1:8765
```

**3. Set up Bunny** (the account API key is used here only; it never needs to live on the server):

```sh
export BUNNY_API_KEY=...                  # Bunny dashboard → Account → API key
birdnet-snapshot bunny-setup -name my-birds -region NY -hostname birds.example.com -env-file snapshot.env
```

This creates or reuses the storage zone, pull zone, cache rules, hostname, CNAME (if the domain is on Bunny DNS) and certificate, then writes the three deploy settings to `snapshot.env`.

**4. Publish, and keep publishing.**

```sh
set -a; . ./snapshot.env; set +a
birdnet-snapshot run -birdnet http://127.0.0.1:8080 -skip-audio doorbell -forbid 'home\.lan'   # once
birdnet-snapshot run -birdnet http://127.0.0.1:8080 -skip-audio doorbell -forbid 'home\.lan' -every 1h  # forever
```

### Running it on a schedule

<details>
<summary><b>Docker Compose</b></summary>

[`deploy/docker-compose.yml`](deploy/docker-compose.yml): copy [`deploy/env.example`](deploy/env.example) to `.env`, fill it in, `docker compose up -d`.

```yaml
services:
  birdnet-snapshot:
    image: ghcr.io/parkwardrr/birdnet-snapshot:latest
    restart: unless-stopped
    shm_size: 1g
    env_file: .env
    environment:
      BIRDNET_URL: http://birdnet-go:8080
      SNAPSHOT_EVERY: 1h
    volumes: [snapshot:/data]
    command: ["run"]
volumes:
  snapshot:
```
</details>

<details>
<summary><b>Podman quadlet</b></summary>

[`deploy/birdnet-snapshot.container`](deploy/birdnet-snapshot.container) → `~/.config/containers/systemd/`, settings in `~/.config/birdnet-snapshot/env`, then `systemctl --user daemon-reload && systemctl --user start birdnet-snapshot`.
</details>

<details>
<summary><b>systemd timer with the binary</b></summary>

[`deploy/systemd/`](deploy/systemd/): a oneshot service plus an hourly timer. Chrome runs as a throwaway container with DevTools bound to `127.0.0.1`, so the host needs only podman.

```sh
install -m 755 birdnet-snapshot ~/.local/bin/
cp deploy/systemd/* ~/.config/systemd/user/
install -m 600 deploy/env.example ~/.config/birdnet-snapshot/env   # then edit
systemctl --user daemon-reload && systemctl --user enable --now birdnet-snapshot.timer
```
</details>

## Commands

| Command | What it does |
|---|---|
| `run` | crawl, then deploy. `-every 1h` keeps running |
| `crawl` | record the guest UI into `-out` |
| `deploy` | sync `-out` to the Bunny storage zone. `-dry` shows the plan |
| `serve` | preview `-out` locally with the CDN's routing rules |
| `check` | test BirdNET-Go, Chrome and Bunny settings |
| `bunny-setup` | create/reuse storage zone, pull zone, cache rules, security headers, write blocking, hostname, DNS, certificate. `-purge` clears the edge cache, `-delete -yes` tears it down |
| `version` | print the version |

## Configuration

Every flag has an environment variable, handy for containers and systemd `EnvironmentFile=`.

| Flag | Env | Default | |
|---|---|---|---|
| `-birdnet` | `BIRDNET_URL` | `http://127.0.0.1:8080` | BirdNET-Go base URL |
| `-out` | `SNAPSHOT_OUT` | `site` | output directory |
| `-days` | `SNAPSHOT_DAYS` | `7` | dashboard days to record |
| `-max-pages` | `SNAPSHOT_MAX_PAGES` | `150` | page visit budget (seed pages always count) |
| `-max-clips` | `SNAPSHOT_MAX_CLIPS` | `1500` | detail pages and clips, newest first |
| `-skip-audio` | `SKIP_AUDIO_SOURCES` | | comma-separated source names whose clips are never published |
| `-no-audio` | `SNAPSHOT_NO_AUDIO` | `false` | publish no clips or spectrograms at all |
| `-routes` | `SNAPSHOT_ROUTES` | | extra UI routes to record |
| `-no-mobile` | `SNAPSHOT_NO_MOBILE` | `false` | skip the phone-layout pass |
| `-label` | `SNAPSHOT_LABEL` | `Snapshot` | text of the corner pill |
| `-location` | `SNAPSHOT_LOCATION` | `hide` | coordinates in recorded data: `hide`, `coarse` (~100 km) or `keep` |
| `-redact` | `SNAPSHOT_REDACT` | | comma-separated regexes replaced with `redacted` |
| `-forbid` | `SNAPSHOT_FORBID` | | comma-separated regexes that fail the build if found in any published file |
| `-no-csp` | `SNAPSHOT_NO_CSP` | `false` | leave out the Content-Security-Policy |
| `-allow-indexing` | `SNAPSHOT_ALLOW_INDEXING` | `false` | let search engines in |
| `-min-entries` | `SNAPSHOT_MIN_ENTRIES` | `20` | refuse to build from a smaller crawl |
| `-max-shrink` | `SNAPSHOT_MAX_SHRINK` | `0.5` | refuse to deploy below this share of the live snapshot (`-force` overrides) |
| `-metrics-file` | `SNAPSHOT_METRICS_FILE` | | Prometheus textfile to write after each run |
| `-every` | `SNAPSHOT_EVERY` | | `run`: repeat at this interval |
| `-chrome` | `CHROME_URL` | | DevTools URL of a running Chrome |
| `-chrome-path` | `CHROME_PATH` | auto | Chrome binary to launch |
| `-no-sandbox` | `CHROME_NO_SANDBOX` | `false` | for Chrome in containers |
| `-zone` / `-zone-host` | `BUNNY_STORAGE_ZONE` / `BUNNY_STORAGE_HOST` | / `storage.bunnycdn.com` | storage zone and its regional endpoint |
| | `BUNNY_STORAGE_KEY` | | storage zone password |
| | `TZ` | auto-detected | BirdNET-Go's timezone |
| `-budget`, `-page-wait`, `-concurrency`, `-addr` | `SNAPSHOT_BUDGET`, … | `20m`, `20s`, `8`, `127.0.0.1:8765` | tuning |

## Privacy and safety

| | |
|---|---|
| **What is published** | Only what a logged-out guest of your BirdNET-Go would see. The crawler rewrites `app/config` to the guest view even if your server has no auth |
| **Never requested** | `/api/v2/settings` (except dashboard display prefs), `system`, `auth`, `streams`, `notifications`, `alerts`, `backup`, `logs`, `integrations`, `import`, `models`, `debug`, `support`, and every SSE stream. Writes are failed inside Chrome before they leave it; the only exception is `POST /api/v2/spectrogram/<id>/generate`, which a guest's browser sends too |
| **Location** | BirdNET-Go puts the station's coordinates in some responses (species rarity, for one). `-location hide` (default) removes every `lat`/`lon`/`latitude`/`longitude` field; `coarse` rounds to whole degrees (~100 km); `keep` leaves them. Sunrise/sunset times and the species list still hint at a region, as on any bird site |
| **Audio** | For detections from sources matching `-skip-audio` (or of unknown source) the clip **and the spectrograms** are withheld (a spectrogram is a picture of the audio and can be turned back into rough sound), including anything the browser preloaded, and their `clipName` is blanked so the UI shows them without a player. `-no-audio` does this for every detection |
| **Stream URLs** | `rtsp://user:pass@host/path` in any response becomes `stream/path` |
| **Your own secrets** | `-redact 'home\.lan,secret-word'` replaces matches in recorded data. `-forbid 'home\.lan,192\.168\.[0-9]+\.[0-9]+'` fails the build if any published file still contains a match |
| **Content-Security-Policy** | `default-src 'self'`, scripts from the site plus the shell's inline scripts by SHA-256, no third-party requests, no frames, plugins or form posts (`-no-csp` to drop it) |
| **Edge headers** (`bunny-setup`) | `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` + `frame-ancestors 'none'`, `Referrer-Policy: no-referrer`, a deny-all `Permissions-Policy`, `Cross-Origin-Opener-Policy` and `-Resource-Policy: same-origin`. `POST`/`PUT`/`PATCH`/`DELETE` are blocked at the CDN; TLS 1.0/1.1 are off |
| **Indexing** | `noindex, nofollow` meta and a deny-all `robots.txt` unless `-allow-indexing` |
| **Secrets** | The server needs only the storage zone password. The account API key is for `bunny-setup`, from your workstation |

## Limits

- Only what the crawl visited exists. Changing an analytics period, typing a search, or paging past what was recorded shows the UI's own "not found" state.
- The dashboard's live widgets (currently hearing, live spectrogram) are removed; live audio is not available.
- The snapshot is English (the UI's labels are matched in English and only those messages are recorded).
- A BirdNET-Go `webserver.basepath` other than `/` is not supported yet; see the [roadmap](ROADMAP.md).
- Built against BirdNET-Go 2026-10 nightlies. A UI change that renames its cache-busting params or English control labels needs a small update here; `check` tells you when the shell can't be parsed.

## Other static hosts

`crawl` output is a plain directory. Hosts need one rule: unknown paths under `/ui/` serve `/ui/index.html` with status 200.

| Host | How |
|---|---|
| Bunny | `bunny-setup` (sets the storage zone's 404 rewrite) |
| Netlify, Cloudflare Pages | `_redirects` is included |
| GitHub Pages | `404.html` is included (served with status 404; browsers don't mind) |
| Caddy | `try_files {path} /ui/index.html` |
| nginx | `location / { try_files $uri /ui/index.html; }` |

Cache `/_snap/*` and `/ui/assets/*.{js,css}` forever (content-addressed); keep everything else, including `snapshot.json`, `sw.js`, HTML and unhashed assets, short (≤ 60 s). Don't cache error responses. Add the security headers from [Privacy and safety](#privacy-and-safety).

## Monitoring

With `-metrics-file`, each run writes (atomically) for the node_exporter textfile collector:

```
birdnet_snapshot_last_success_timestamp_seconds   birdnet_snapshot_last_run_success
birdnet_snapshot_run_duration_seconds             birdnet_snapshot_crawl_duration_seconds
birdnet_snapshot_deploy_duration_seconds          birdnet_snapshot_pages
birdnet_snapshot_entries                          birdnet_snapshot_detections
birdnet_snapshot_clips                            birdnet_snapshot_files
birdnet_snapshot_bytes                            birdnet_snapshot_uploaded_files
birdnet_snapshot_uploaded_bytes                   birdnet_snapshot_deleted_files
birdnet_snapshot_info{version="..."}
```

```yaml
- alert: BirdnetSnapshotStale
  expr: time() - birdnet_snapshot_last_success_timestamp_seconds > 3 * 3600
  for: 10m
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `check` fails on chrome | install Chrome/Chromium, set `-chrome-path`, or point `-chrome` at a running one. In containers add `-no-sandbox` (the image sets it) |
| `only N API responses recorded` | BirdNET-Go was down or restarting; nothing was published. Check `-birdnet` |
| `not deploying (use -force…)` | the new crawl is much smaller than the live one. Look at the log, then `-force` if it's intended |
| Dashboard shows the wrong day | set `TZ` to BirdNET-Go's zone (it is auto-detected from detection timestamps otherwise) |
| Visitors see an old snapshot | entry files are cached 60 s at the edge; the service worker re-checks every minute |
| `zone rejected the key` | use the storage zone password (`BUNNY_STORAGE_KEY`), not the account API key |

## Testing

```sh
go test -short ./...      # unit tests and fake Bunny storage/account APIs (no network, no Chrome)
go test ./...             # + end-to-end: a fake BirdNET-Go crawled by real Chrome, built, served and browsed
cd tests/browsers && go run . -url https://birds.example.com   # Chromium, Firefox and WebKit (Playwright)
```

- **End-to-end** (`e2e_test.go`, chromedp): no denied endpoint or write reaches the server, stream URLs and coordinates are scrubbed, skipped clips are absent, species images with spaces load, lazy chunks the crawl never loaded are present, the CSP lets the shell's inline script run, protected endpoints get `401`, writes `403`, spectrogram status is answered from the recording, detail pages never opened by the crawl work through the learned templates, and the holding page shows before the first snapshot.
- **Browsers** (`tests/browsers`, Playwright): loads a snapshot in Chromium, Firefox and WebKit at desktop and phone sizes, first and repeat visit, and fails on anything a visitor would notice: error toasts, page errors, console errors, broken images, missing assets, a page the service worker doesn't control. Expected guest behaviour (`401` on protected endpoints, `403` on writes) is ignored; data missing from the snapshot is reported as a warning.
- **CI** runs on a self-hosted runner (rootless Podman). Every job is pinned to `runs-on: [self-hosted, cairn]` and carries a same-repository guard so a fork's pull request never reaches it; `tests/check-runners.sh` enforces that, and outside contributors need approval. The release job builds the binaries and the multi-arch image without a container daemon (`tools/mkimage`). `site.yml` runs the browser check against the live snapshot every 6 hours when the `SITE_URL` variable is set.

## License

[Blue Oak Model License 1.0.0](LICENSE.md) for this tool.

A published snapshot also contains BirdNET-Go's frontend files, which are under BirdNET-Go's own license ([CC BY-NC-SA 4.0](https://github.com/tphakala/birdnet-go/blob/main/LICENSE)): keep snapshots non-commercial and keep the attribution the UI already shows. birdnet-snapshot contains no BirdNET-Go code and is not affiliated with the BirdNET-Go project or the Cornell Lab of Ornithology.
