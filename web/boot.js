// Starts the BirdNET-Go UI inside the snapshot.
//
// Runs before the app: stubs EventSource (there are no live streams), waits
// for the snapshot service worker to control the page, pins the clock to the
// snapshot time so "today" and "x minutes ago" match the recorded data, then
// imports the app entry. Shows a holding page until a snapshot exists.
(function () {
  'use strict';
  var script = document.currentScript;
  var entry = script.getAttribute('data-entry');

  // Only the English messages are recorded, and snapshot.css targets English
  // labels. Set before the shell's own locale detection runs.
  try {
    localStorage.setItem('birdnet-locale', 'en');
  } catch (e) {}

  // No live streams in a snapshot: an EventSource that never connects.
  function SnapshotEventSource(url) {
    this.url = String(url);
    this.readyState = 0;
    this.withCredentials = false;
    this.onopen = this.onmessage = this.onerror = null;
    this._t = document.createElement('span');
  }
  SnapshotEventSource.prototype.addEventListener = function (t, f, o) { this._t.addEventListener(t, f, o); };
  SnapshotEventSource.prototype.removeEventListener = function (t, f, o) { this._t.removeEventListener(t, f, o); };
  SnapshotEventSource.prototype.dispatchEvent = function (e) { return this._t.dispatchEvent(e); };
  SnapshotEventSource.prototype.close = function () { this.readyState = 2; };
  SnapshotEventSource.CONNECTING = 0;
  SnapshotEventSource.OPEN = 1;
  SnapshotEventSource.CLOSED = 2;
  window.EventSource = SnapshotEventSource;

  function overlay(html) {
    var el = document.getElementById('snapshot-splash');
    if (!el) {
      el = document.createElement('div');
      el.id = 'snapshot-splash';
      (document.body || document.documentElement).appendChild(el);
    }
    el.innerHTML = '<div class="snapshot-card"><img src="/ui/assets/apple-touch-icon.png" alt="" width="64" height="64">' + html + '</div>';
  }

  function pinClock(generatedAt) {
    var RealDate = Date;
    var t0 = performance.now();
    var now = function () { return generatedAt + (performance.now() - t0); };
    function SnapshotDate() {
      if (!(this instanceof SnapshotDate)) return new RealDate(now()).toString();
      var a = arguments;
      if (a.length === 0) return new RealDate(now());
      return new (Function.prototype.bind.apply(RealDate, [null].concat(Array.prototype.slice.call(a))))();
    }
    SnapshotDate.prototype = RealDate.prototype;
    SnapshotDate.now = now;
    SnapshotDate.parse = RealDate.parse;
    SnapshotDate.UTC = RealDate.UTC;
    window.Date = SnapshotDate;
  }

  function banner(info) {
    var d = new Date(info.generatedAt);
    var el = document.createElement('a');
    el.id = 'snapshot-banner';
    el.href = '/ui/dashboard';
    el.title = 'Read-only snapshot of BirdNET-Go';
    el.textContent = (info.label || 'Snapshot') + ' · ' + d.toLocaleString([], { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
    document.body.appendChild(el);
  }

  // Default the dashboard to the snapshot's day rather than the visitor's.
  function pinDashboardDate(info) {
    var p = location.pathname.replace(/\/+$/, '');
    if ((p === '/ui' || p === '/ui/dashboard') && !new URLSearchParams(location.search).has('date')) {
      history.replaceState(history.state, '', '/ui/dashboard?date=' + info.serverToday + location.hash);
    }
  }

  function controlled() {
    if (navigator.serviceWorker.controller) return Promise.resolve();
    return new Promise(function (resolve) {
      navigator.serviceWorker.addEventListener('controllerchange', resolve, { once: true });
      navigator.serviceWorker.ready.then(function (reg) {
        if (reg.active) reg.active.postMessage('claim');
      });
      setTimeout(function () {
        // A hard reload bypasses the worker for this load; reload once normally.
        if (!navigator.serviceWorker.controller && !sessionStorage.getItem('snapshot-reloaded')) {
          sessionStorage.setItem('snapshot-reloaded', '1');
          location.reload();
        }
      }, 4000);
    });
  }

  async function start() {
    if (!('serviceWorker' in navigator)) {
      overlay('<h1>BirdNET-Go</h1><p>This page needs a current browser with service workers enabled.</p>');
      return;
    }
    overlay('<h1>BirdNET-Go</h1><p>Loading the latest snapshot…</p>');
    var info;
    try {
      var r = await fetch('/snapshot.json', { cache: 'no-store' });
      if (!r.ok) throw new Error(r.status);
      info = await r.json();
    } catch (e) {
      overlay('<h1>BirdNET-Go</h1><p>The first snapshot is being prepared.<br>Check back in a few minutes.</p>');
      setTimeout(function () { location.reload(); }, 60000);
      return;
    }
    await navigator.serviceWorker.register('/sw.js', { scope: '/' });
    await controlled();
    sessionStorage.removeItem('snapshot-reloaded');
    pinClock(info.generatedAt);
    pinDashboardDate(info);
    await import(entry);
    banner(info);
    var splash = document.getElementById('snapshot-splash');
    if (splash) splash.remove();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
