package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// fakeBirdNET mimics enough of BirdNET-Go for an end-to-end crawl: an SPA
// shell whose module entry calls the API like the real UI does, including
// cache busters, an EventSource, a forbidden settings call and a write.
type fakeBirdNET struct {
	mu   sync.Mutex
	hits []string
}

var fakeDetections = []map[string]any{
	{"id": 31, "commonName": "Great Horned Owl", "scientificName": "Bubo virginianus", "clipName": "owl.flac",
		"source": map[string]any{"id": "rtsp://admin:pw@nvr.lan:8554/yard?audio", "displayName": "Yard"}},
	{"id": 32, "commonName": "House Finch", "scientificName": "Haemorhous mexicanus", "clipName": "finch.flac",
		"source": map[string]any{"id": "rtsp://admin:pw@nvr.lan:8554/doorbell?audio", "displayName": "Doorbell"}},
	{"id": 33, "commonName": "Black Phoebe", "scientificName": "Sayornis nigricans", "clipName": "phoebe.flac",
		"source": map[string]any{"id": "rtsp://admin:pw@nvr.lan:8554/yard?audio", "displayName": "Yard"}},
}

const fakeApp = `
const app = document.getElementById('app');
const cfg = await (await fetch('/api/v2/app/config')).json();
window.__guest = cfg.security.enabled && !cfg.security.accessAllowed;
new EventSource('/api/v2/detections/stream');
fetch('/api/v2/settings/main').catch(() => {});
window.__write = await fetch('/api/v2/detections/31', { method: 'DELETE' }).then(r => r.status, () => 'blocked');
const m = location.pathname.match(/^\/ui\/detections\/(\d+)$/);
if (m) {
  const d = await (await fetch('/api/v2/detections/' + m[1])).json();
  window.__specStatus = await fetch('/api/v2/spectrogram/' + d.id + '/status?size=lg').then(r => r.ok ? r.json().then(j => j.data.status) : r.status);
  window.__missingStatus = await fetch('/api/v2/spectrogram/999/status?size=lg').then(r => r.status);
  app.innerHTML = '<h1 id="title">' + d.commonName + '</h1><img id="spec" src="/api/v2/spectrogram/' + d.id + '?size=lg&cache=' + Date.now() + '">';
  await new Promise(r => { const i = document.getElementById('spec'); i.onload = i.onerror = r; });
  window.__specWidth = document.getElementById('spec').naturalWidth;
  window.__audio = await fetch('/api/v2/audio/' + d.id + '?t=' + Date.now()).then(r => r.status);
} else {
  const date = new URLSearchParams(location.search).get('date') || 'none';
  const list = await (await fetch('/api/v2/detections/recent?limit=5&date=' + date + '&t=' + Date.now())).json();
  app.innerHTML = '<h1 id="title">Dashboard ' + date + '</h1><ul>' +
    list.map(d => '<li class="det">' + d.commonName + '</li>').join('') + '</ul>' +
    list.map(d => '<a href="/ui/detections?queryType=species&species=' + encodeURIComponent(d.scientificName) + '&date=' + date + '">' + d.commonName + '</a>').join('');
}
app.dataset.ready = '1';
`

// A 1x1 PNG.
var pngPixel = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func (f *fakeBirdNET) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/ui/assets/index-e2e.js"):
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprint(w, fakeApp)
	case strings.HasPrefix(p, "/ui/assets/"):
		http.NotFound(w, r)
	case strings.HasPrefix(p, "/ui"):
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, strings.Replace(testShell, "/ui/assets/index-abc.js", "/ui/assets/index-e2e.js", 1))
	case p == "/api/v2/app/config":
		writeJSON(w, map[string]any{"csrfToken": "tok", "version": "e2e", "security": map[string]any{"enabled": false, "accessAllowed": true}})
	case p == "/api/v2/detections/recent":
		writeJSON(w, fakeDetections)
	case strings.HasPrefix(p, "/api/v2/detections/"):
		for _, d := range fakeDetections {
			if p == fmt.Sprintf("/api/v2/detections/%v", d["id"]) {
				writeJSON(w, d)
				return
			}
		}
		http.NotFound(w, r)
	case strings.HasSuffix(p, "/status"):
		writeJSON(w, map[string]any{"data": map[string]any{"status": "generating"}}) // still rendering during the crawl
	case strings.HasPrefix(p, "/api/v2/spectrogram/"):
		for _, d := range fakeDetections {
			if p == fmt.Sprintf("/api/v2/spectrogram/%v", d["id"]) {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(pngPixel)
				return
			}
		}
		http.NotFound(w, r) // like BirdNET-Go for an unknown detection
	case strings.HasPrefix(p, "/api/v2/audio/"):
		w.Header().Set("Content-Type", "audio/flac")
		fmt.Fprint(w, "fLaC-"+strings.TrimPrefix(p, "/api/v2/audio/"))
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeBirdNET) hit(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.hits {
		if strings.HasPrefix(h, prefix) {
			return true
		}
	}
	return false
}

// chromeOrSkip skips the test when no Chrome can be launched.
func chromeOrSkip(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end test needs Chrome; skipped with -short")
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1280, 1000))
	if p := os.Getenv("CHROME_PATH"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	if envBool("CHROME_NO_SANDBOX", false) {
		opts = append(opts, chromedp.NoSandbox)
	}
	alloc, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(cancel)
	ctx, cancel2 := chromedp.NewContext(alloc)
	t.Cleanup(cancel2)
	if err := chromedp.Run(ctx); err != nil {
		t.Skipf("no Chrome available: %v", err)
	}
	return ctx
}

func TestEndToEnd(t *testing.T) {
	browserCtx := chromeOrSkip(t)

	fb := &fakeBirdNET{}
	birdnet := httptest.NewServer(fb)
	defer birdnet.Close()

	out := filepath.Join(t.TempDir(), "site")
	c := config{
		BirdNET: birdnet.URL, Out: out, Days: 1, MaxPages: 12, MaxClips: 10, Label: "E2E",
		SkipAudio: []string{"doorbell"}, PageWait: 5 * time.Second, Budget: 3 * time.Minute, MinEntries: 3,
		ChromePath: os.Getenv("CHROME_PATH"), NoSandbox: envBool("CHROME_NO_SANDBOX", false),
	}
	ctx := context.Background()
	res, err := crawlAndBuild(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("crawl: %+v", *res)

	// What reached the server.
	if fb.hit("GET /api/v2/settings") {
		t.Error("crawl requested a denied settings endpoint")
	}
	if fb.hit("DELETE ") {
		t.Error("a write reached the server")
	}
	if fb.hit("GET /api/v2/detections/stream") {
		t.Error("the live stream reached the server")
	}

	// What was published.
	var info snapshotInfo
	readJSON(t, filepath.Join(out, "snapshot.json"), &info)
	var m manifest
	readJSON(t, filepath.Join(out, strings.TrimPrefix(info.Manifest, "/")), &m)
	today := time.Now().Format("2006-01-02")
	for _, k := range []string{
		"/api/v2/app/config",
		"/api/v2/detections/recent?limit=5&date=" + today,
		"/api/v2/detections/31", "/api/v2/detections/33",
		"/api/v2/audio/31", "/api/v2/audio/33",
		"/api/v2/spectrogram/31?size=lg",
	} {
		if _, ok := m.Entries[k]; !ok {
			t.Errorf("manifest lacks %s", k)
		}
	}
	for k := range m.Entries {
		if strings.Contains(k, "/api/v2/audio/32") || strings.Contains(k, "settings/main") || regexp.MustCompile(`[?&](t|cache|retry)=`).MatchString(k) {
			t.Errorf("manifest has %s", k)
		}
	}
	cfgFile, _ := os.ReadFile(filepath.Join(out, "_snap", m.Entries["/api/v2/app/config"].F))
	if !strings.Contains(string(cfgFile), `"accessAllowed":false`) || strings.Contains(string(cfgFile), "tok") {
		t.Errorf("app/config not rewritten to guest: %s", cfgFile)
	}
	recent, _ := os.ReadFile(filepath.Join(out, "_snap", m.Entries["/api/v2/detections/recent?limit=5&date="+today].F))
	if strings.Contains(string(recent), "nvr.lan") || strings.Contains(string(recent), "pw@") {
		t.Errorf("stream URL published: %s", recent)
	}

	// Serve the snapshot with the BirdNET-Go server gone, and browse it.
	birdnet.Close()
	site := httptest.NewServer(serveHandler(out))
	defer site.Close()

	tab, cancel := context.WithTimeout(browserCtx, 90*time.Second)
	defer cancel()
	var (
		title, banner string
		dets          []string
		guest         bool
		write         any
		controlled    bool
	)
	err = chromedp.Run(tab,
		chromedp.Navigate(site.URL+"/ui/dashboard"),
		chromedp.WaitVisible(`#app[data-ready="1"]`, chromedp.ByQuery),
		chromedp.Text(`#title`, &title, chromedp.ByQuery),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.det'), e => e.textContent)`, &dets),
		chromedp.WaitVisible(`#snapshot-banner`, chromedp.ByQuery),
		chromedp.Text(`#snapshot-banner`, &banner, chromedp.ByQuery),
		chromedp.Evaluate(`window.__guest`, &guest),
		chromedp.Evaluate(`window.__write`, &write),
		chromedp.Evaluate(`!!navigator.serviceWorker.controller`, &controlled),
	)
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	if title != "Dashboard "+info.ServerToday {
		t.Errorf("dashboard not pinned to the snapshot day: %q", title)
	}
	if len(dets) != 3 || !controlled || !guest {
		t.Errorf("dashboard: dets %v, controlled %v, guest %v", dets, controlled, guest)
	}
	if !strings.HasPrefix(banner, "E2E · ") {
		t.Errorf("banner %q", banner)
	}
	if fmt.Sprint(write) != "403" {
		t.Errorf("write should get 403 from the service worker, got %v", write)
	}

	var specWidth, audio float64
	var specStatus, missingStatus any
	err = chromedp.Run(tab,
		chromedp.Navigate(site.URL+"/ui/detections/33"),
		chromedp.WaitVisible(`#app[data-ready="1"]`, chromedp.ByQuery),
		chromedp.Text(`#title`, &title, chromedp.ByQuery),
		chromedp.Evaluate(`window.__specStatus`, &specStatus),
		chromedp.Evaluate(`window.__missingStatus`, &missingStatus),
		chromedp.Evaluate(`window.__specWidth`, &specWidth),
		chromedp.Evaluate(`window.__audio`, &audio),
	)
	if err != nil {
		t.Fatalf("detail page: %v", err)
	}
	if fmt.Sprint(specStatus) != "exists" || fmt.Sprint(missingStatus) != "404" {
		t.Errorf("spectrogram status: recorded one %v (want exists), missing one %v (want 404)", specStatus, missingStatus)
	}
	if title != "Black Phoebe" || specWidth != 1 || audio != 200 {
		t.Errorf("detail page: title %q, spectrogram width %v, audio %v", title, specWidth, audio)
	}

	// The doorbell detection's clip is absent: 404 from the service worker.
	err = chromedp.Run(tab,
		chromedp.Navigate(site.URL+"/ui/detections/32"),
		chromedp.WaitVisible(`#app[data-ready="1"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__audio`, &audio),
	)
	if err != nil {
		t.Fatalf("doorbell detail page: %v", err)
	}
	if audio != 404 {
		t.Errorf("doorbell clip should be 404, got %v", audio)
	}
}

// The holding page shows while no snapshot has been published.
func TestEndToEndHoldingPage(t *testing.T) {
	browserCtx := chromeOrSkip(t)
	_, dir := buildTestSite(t, nil)
	if err := os.Remove(filepath.Join(dir, "snapshot.json")); err != nil {
		t.Fatal(err)
	}
	site := httptest.NewServer(serveHandler(dir))
	defer site.Close()
	tab, cancel := context.WithTimeout(browserCtx, 30*time.Second)
	defer cancel()
	var text string
	err := chromedp.Run(tab,
		chromedp.Navigate(site.URL+"/ui/dashboard"),
		chromedp.WaitVisible(`#snapshot-splash`, chromedp.ByQuery),
		chromedp.Poll(`document.getElementById('snapshot-splash').innerText.includes('being prepared')`, nil),
		chromedp.Text(`#snapshot-splash`, &text, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "first snapshot is being prepared") {
		t.Errorf("holding page text %q", text)
	}
}
