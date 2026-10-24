// BirdNET-Go snapshot service worker.
//
// The static site holds the real BirdNET-Go UI plus recorded API responses.
// This worker answers the UI's /api/ requests (and any asset requested with a
// query string) from /_snap/, using the manifest named in /snapshot.json.
// Everything is read-only: writes get a 403 and unrecorded API calls a 404.

const SNAPSHOT = '/snapshot.json';
const RECHECK_MS = 60 * 1000;

let state = null; // { info, entries }
let lastCheck = 0;
let loading = null;

async function load(force) {
  if (state && !force && Date.now() - lastCheck < RECHECK_MS) return state;
  if (loading) return loading;
  loading = (async () => {
    try {
      const info = await (await fetch(SNAPSHOT, { cache: 'no-store' })).json();
      lastCheck = Date.now();
      if (!state || state.info.manifest !== info.manifest) {
        const m = await (await fetch(info.manifest)).json();
        state = { info, entries: m.entries };
      }
    } catch (e) {
      // keep serving the previous snapshot if the refresh failed
    }
    loading = null;
    return state;
  })();
  return loading;
}

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));
self.addEventListener('message', (e) => {
  if (e.data === 'claim') self.clients.claim();
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname === SNAPSHOT || url.pathname.startsWith('/_snap/') || url.pathname === '/sw.js') return;
  if (e.request.mode === 'navigate') {
    load(false); // refresh in the background on every page load
    return;
  }
  const api = url.pathname.startsWith('/api/');
  if (!api && !url.search) return;
  e.respondWith(answer(e.request, url, api));
});

async function answer(req, url, api) {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    return json(403, { error: 'read_only', message: 'This is a read-only snapshot of BirdNET-Go.' });
  }
  const st = await load(false);
  // The UI asks whether a spectrogram is rendered before loading it. Answer
  // from the recording: "exists" if the image is in the snapshot, otherwise
  // 404 so the UI shows "unavailable" instead of polling forever.
  const status = /^\/api\/v2\/spectrogram\/(\d+)\/status$/.exec(url.pathname);
  if (status) {
    const img = key(new URL('/api/v2/spectrogram/' + status[1] + url.search, url));
    return st && st.entries[img]
      ? json(200, { data: { status: 'exists', queuePosition: 0 } })
      : json(404, { error: 'not_in_snapshot', message: 'Spectrogram not included in this snapshot.' });
  }
  const entry = st && st.entries[key(url)];
  if (entry) {
    const r = await fetch('/_snap/' + entry.f);
    if (r.ok) return respond(req, await r.blob(), entry.t);
  }
  if (!api) return fetch(url.pathname); // query string on a static file: ignore it
  return json(404, { error: 'not_in_snapshot', message: 'Not included in this snapshot.' });
}

// respond serves a body, honouring Range so Safari will play audio clips.
function respond(req, blob, type) {
  const headers = { 'Content-Type': type, 'Accept-Ranges': 'bytes', 'Cache-Control': 'no-store' };
  const range = req.headers.get('Range');
  const m = range && /bytes=(\d*)-(\d*)/.exec(range);
  if (!m) return new Response(blob, { status: 200, headers: { ...headers, 'Content-Length': String(blob.size) } });
  let start = m[1] === '' ? blob.size - Number(m[2]) : Number(m[1]);
  let end = m[1] !== '' && m[2] !== '' ? Number(m[2]) : blob.size - 1;
  start = Math.max(0, start);
  end = Math.min(end, blob.size - 1);
  if (start > end) {
    return new Response(null, { status: 416, headers: { 'Content-Range': `bytes */${blob.size}` } });
  }
  return new Response(blob.slice(start, end + 1), {
    status: 206,
    headers: {
      ...headers,
      'Content-Range': `bytes ${start}-${end}/${blob.size}`,
      'Content-Length': String(end - start + 1),
    },
  });
}

// Cache busters the UI appends; must match volatileParams in crawl.go.
const VOLATILE = new Set(['t', 'cache', 'retry']);

// key is the manifest key of a URL: path plus raw query minus cache busters.
function key(url) {
  const keep = url.search
    .slice(1)
    .split('&')
    .filter((p) => p !== '' && !VOLATILE.has(p.split('=')[0]));
  return keep.length ? url.pathname + '?' + keep.join('&') : url.pathname;
}

function json(status, body) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}
