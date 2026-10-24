// BirdNET-Go snapshot service worker.
//
// The static site holds the real BirdNET-Go UI plus recorded API responses.
// This worker answers the UI's /api/ requests (and any asset requested with a
// query string) from /_snap/, using the manifest named in /snapshot.json.
// Everything is read-only: writes get a 403 and unrecorded API calls a 404.

const SNAPSHOT = '/snapshot.json';
const RECHECK_MS = 60 * 1000;
// Used until a snapshot.json that lists them is loaded (older snapshots).
const DEFAULT_PROTECTED = ['/api/v2/settings', '/api/v2/system', '/api/v2/auth', '/api/v2/streams', '/api/v2/notifications', '/api/v2/alerts'];

let state = null; // { info, entries }
let previous = null; // { manifest, entries } of the snapshot before, for pages still running its code
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

// Load the manifest while installing, so the first API call doesn't wait for it.
self.addEventListener('install', (e) => e.waitUntil(load(true).then(() => self.skipWaiting())));
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
  // Always answer: a rejected promise here is a network error in the page
  // (Firefox logs it as "ServiceWorker encountered an unexpected error").
  e.respondWith(
    answer(e.request, url, api).catch(() =>
      json(503, { error: 'unavailable', message: 'The snapshot could not be loaded. Check your connection.' })
    )
  );
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
  const k = key(url);
  let entry = st && st.entries[k];
  if (!entry && st && st.info.previous) entry = (await loadPrevious(st.info.previous))[k];
  if (entry) {
    try {
      const r = await fetch('/_snap/' + entry.f);
      if (r.ok) return respond(req, await r.blob(), entry.t);
    } catch (e) {
      return json(503, { error: 'unavailable', message: 'The snapshot could not be loaded. Check your connection.' });
    }
  }
  if (!api) return fetch(url.pathname); // query string on a static file: ignore it (errors caught above)
  // Endpoints a logged-out guest of a real server gets 401 for: answer the
  // same, so the UI treats it as "not signed in" and stays quiet.
  const prot = (st && st.info.protected) || DEFAULT_PROTECTED;
  if (prot.some((p) => url.pathname === p || url.pathname.startsWith(p + '/'))) {
    return json(401, { error: 'unauthorized', message: 'Sign-in required.' });
  }
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

// loadPrevious returns the entries of the snapshot this one replaced. A page
// that loaded the previous build (the edge caches the shell for a minute after
// a BirdNET-Go upgrade) asks for what that build recorded.
async function loadPrevious(manifest) {
  if (previous && previous.manifest === manifest) return previous.entries;
  try {
    const m = await (await fetch(manifest)).json();
    previous = { manifest, entries: m.entries };
  } catch (e) {
    previous = { manifest, entries: {} };
  }
  return previous.entries;
}

function json(status, body) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}
