package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestKeyDropsCacheBusters(t *testing.T) {
	cases := map[string]string{
		"http://h/api/v2/spectrogram/12?size=lg&cache=0&retry=4":  "/api/v2/spectrogram/12?size=lg",
		"http://h/api/v2/audio/5?t=1792861122906":                 "/api/v2/audio/5",
		"http://h/api/v2/x?b=2&a=1":                               "/api/v2/x?b=2&a=1", // order kept
		"http://h/api/v2/media/species-image?name=Corvus%20corax": "/api/v2/media/species-image?name=Corvus%20corax",
		"http://h/ui/assets/app.js":                               "/ui/assets/app.js",
		"http://h/api/v2/y?tt=1&t=2&cached=3":                     "/api/v2/y?tt=1&cached=3", // whole names only
	}
	for in, want := range cases {
		if got := key(mustURL(t, in)); got != want {
			t.Errorf("key(%s) = %s, want %s", in, got, want)
		}
	}
}

// The service worker must strip exactly the same params as crawl.go.
func TestVolatileParamsMatchServiceWorker(t *testing.T) {
	sw, err := webFS.ReadFile("web/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const VOLATILE = new Set\(\[([^\]]*)\]\)`).FindSubmatch(sw)
	if m == nil {
		t.Fatal("VOLATILE set not found in web/sw.js")
	}
	var js []string
	for _, p := range strings.Split(string(m[1]), ",") {
		js = append(js, strings.Trim(strings.TrimSpace(p), `'"`))
	}
	var gos []string
	for k := range volatileParams {
		gos = append(gos, k)
	}
	sort.Strings(js)
	sort.Strings(gos)
	if strings.Join(js, ",") != strings.Join(gos, ",") {
		t.Fatalf("sw.js VOLATILE %v != crawl.go volatileParams %v", js, gos)
	}
}

func TestLinkClass(t *testing.T) {
	cases := map[string]string{
		"/ui/dashboard?date=2026-10-24":                                                "dashboard",
		"/ui/detections?queryType=species&species=Bubo+virginianus&date=2026-10-24":    "species",
		"/ui/detections?queryType=species&species=X&date=2026-10-24&hour=7&duration=1": "species-hour",
		"/ui/detections?queryType=hourly&date=2026-10-24&hour=3&duration=1":            "hourly",
		"/ui/analytics/trends":                  "other",
		"/ui/detections?queryType=search&q=owl": "other",
	}
	for in, want := range cases {
		if got := linkClass(in); got != want {
			t.Errorf("linkClass(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestDeniedPath(t *testing.T) {
	denied := []string{
		"/api/v2/settings", "/api/v2/settings/main", "/api/v2/system/resources", "/api/v2/auth/login",
		"/api/v2/streams/hls/x.m3u8", "/api/v2/detections/stream", "/api/v2/notifications", "/sw.js",
		"/api/v2/backup/jobs", "/api/v2/logs",
	}
	allowed := []string{
		"/api/v2/settings/dashboard", "/api/v2/detections/recent", "/api/v2/detections/12",
		"/api/v2/analytics/species/daily", "/api/v2/spectrogram/4", "/api/v2/app/config",
		"/api/v2/streamsx", "/ui/assets/sw.js",
	}
	for _, p := range denied {
		if !deniedPath(p) {
			t.Errorf("%s should be denied", p)
		}
	}
	for _, p := range allowed {
		if deniedPath(p) {
			t.Errorf("%s should be allowed", p)
		}
	}
}

func TestAllowedWrite(t *testing.T) {
	if !allowedWrite.MatchString("/api/v2/spectrogram/123/generate") {
		t.Error("spectrogram generate should pass")
	}
	for _, p := range []string{"/api/v2/detections/1", "/api/v2/spectrogram/1/generate/x", "/api/v2/settings"} {
		if allowedWrite.MatchString(p) {
			t.Errorf("%s must not pass", p)
		}
	}
}

func TestGuestJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"csrfToken":"secret","security":{"enabled":false,"accessAllowed":true},
			"newVersion":true,"freshInstall":true,"audioExportEnabled":true,"liveSpectrogram":true,"version":"1.0",
			"layout":{"elements":[{"type":"daily-summary"},{"type":"currently-hearing"},{"type":"live-spectrogram"},{"type":"detections-grid"}]}}`))
	}))
	defer srv.Close()

	b, err := guestJSON(srv.Client(), srv.URL+"/api/v2/app/config")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	sec := cfg["security"].(map[string]any)
	if cfg["csrfToken"] != "" || sec["enabled"] != true || sec["accessAllowed"] != false {
		t.Errorf("not a guest config: %s", b)
	}
	if cfg["newVersion"] != false || cfg["audioExportEnabled"] != false || cfg["liveSpectrogram"] != false {
		t.Errorf("flags not reset: %s", b)
	}
	if cfg["version"] != "1.0" {
		t.Error("unrelated fields must survive")
	}
	els := cfg["layout"].(map[string]any)["elements"].([]any)
	if len(els) != 2 {
		t.Errorf("live widgets not removed: %v", els)
	}

	// settings/dashboard keeps its own fields and loses only live widgets.
	b, err = guestJSON(srv.Client(), srv.URL+"/api/v2/settings/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &cfg)
	if cfg["csrfToken"] != "secret" {
		t.Error("settings/dashboard must not get the app/config rewrite")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) }))
	defer bad.Close()
	if _, err := guestJSON(bad.Client(), bad.URL+"/api/v2/app/config"); err == nil {
		t.Error("want error on 500")
	}
}

func TestSanitize(t *testing.T) {
	in := &capture{MIME: "application/json", Body: []byte(`{"source":{"id":"rtsp://admin:hunter2@nvr.lan:8554/garage_front?audio","displayName":"Garage"},"x":"rtsps://cam/yard"}`)}
	out := string(sanitize(in))
	if strings.Contains(out, "hunter2") || strings.Contains(out, "nvr.lan") || strings.Contains(out, "rtsp") {
		t.Errorf("stream URL leaked: %s", out)
	}
	if !strings.Contains(out, `"stream/garage_front"`) || !strings.Contains(out, `"stream/yard"`) {
		t.Errorf("stream name lost: %s", out)
	}
	raw := &capture{MIME: "image/png", Body: []byte("rtsp://keep/me")}
	if string(sanitize(raw)) != "rtsp://keep/me" {
		t.Error("non-JSON bodies must be untouched")
	}
}

const testShell = `<!doctype html>
<html><head>
<title>BirdNET-Go</title>
<script type="module" crossorigin src="/ui/assets/index-abc.js"></script>
</head><body><div id="app"></div>
    <script>
      if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/sw.js', { scope: '/' });
      }
    </script>
</body></html>`

func TestShell(t *testing.T) {
	out, err := shell([]byte(testShell), false, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`data-entry="/ui/assets/index-abc.js"`, `/_snapshot/boot.js`, `/_snapshot/snapshot.css`, `noindex`} {
		if !strings.Contains(s, want) {
			t.Errorf("shell lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, `type="module"`) || strings.Contains(s, "navigator.serviceWorker.register") {
		t.Errorf("entry or app service worker not removed:\n%s", s)
	}
	out, _ = shell([]byte(testShell), true, true)
	if strings.Contains(string(out), "noindex") {
		t.Error("allowIndex must drop the robots meta")
	}
	if _, err := shell([]byte("<html><head></head></html>"), false, true); err == nil {
		t.Error("want error without a module entry")
	}
}

func TestExtFor(t *testing.T) {
	cases := map[string]string{
		"application/json; charset=utf-8": ".json", "image/png": ".png", "audio/flac": ".flac",
		"audio/x-flac": ".flac", "IMAGE/JPEG": ".jpg", "application/x-weird": ".bin", "": ".bin",
	}
	for in, want := range cases {
		if got := extFor(in); got != want {
			t.Errorf("extFor(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestCollectDetectionsAndSkip(t *testing.T) {
	var v any
	_ = json.Unmarshal([]byte(`{"data":[
		{"id":1,"clipName":"a.flac","source":{"id":"rtsp://x/doorbell?audio","displayName":"Doorbell"}},
		{"id":2,"clipName":"b.flac","source":{"id":"rtsp://x/yard","displayName":"Yard"}},
		{"id":3,"clipName":"","source":"malgo:default"},
		{"id":4,"commonName":"no clip field"},
		{"nested":{"id":5,"clipName":"e.flac","source":"sysdefault"}}]}`), &v)
	dets := map[int64]detection{}
	collectDetections(v, dets)
	if len(dets) != 4 {
		t.Fatalf("want 4 detections, got %v", dets)
	}
	if !skipSource(dets[1].Source, []string{"doorbell"}) {
		t.Error("doorbell clip should be skipped")
	}
	if skipSource(dets[2].Source, []string{"doorbell"}) {
		t.Error("yard clip should be kept")
	}
	if !skipSource("", nil) {
		t.Error("unknown source must be skipped")
	}
}

func newTestStore() *store {
	return &store{items: map[string]*capture{}, blocked: map[string]int{}, byPage: map[string][]string{}}
}

func TestValidate(t *testing.T) {
	c := config{BirdNET: "http://x", MinEntries: 3}
	st := newTestStore()
	if err := validate(c, st); err == nil || !strings.Contains(err.Error(), "SPA shell") {
		t.Errorf("want missing shell error, got %v", err)
	}
	st.index = []byte(testShell)
	if err := validate(c, st); err == nil || !strings.Contains(err.Error(), "app/config") {
		t.Errorf("want missing config error, got %v", err)
	}
	st.put(&capture{Key: "/api/v2/app/config", MIME: "application/json", Body: []byte("{}")})
	st.put(&capture{Key: "/ui/assets/a.js", MIME: "text/javascript", Body: []byte("x")})
	if err := validate(c, st); err == nil || !strings.Contains(err.Error(), "only 1") {
		t.Errorf("want min entries error, got %v", err)
	}
	st.put(&capture{Key: "/api/v2/detections/recent", MIME: "application/json", Body: []byte("[]")})
	st.put(&capture{Key: "/api/v2/health", MIME: "application/json", Body: []byte("{}")})
	if err := validate(c, st); err != nil {
		t.Errorf("want ok, got %v", err)
	}
}

// buildTestSite builds a small snapshot into a temp dir and returns it.
func buildTestSite(t *testing.T, extra map[string]string) (config, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "site")
	c := config{Out: dir, Label: "Test snapshot", MinEntries: 1}
	st := newTestStore()
	st.index = []byte(testShell)
	st.visited = []string{"/ui/dashboard"}
	st.put(&capture{Key: "/ui/assets/index-abc.js", MIME: "text/javascript", Body: []byte("console.log(1)")})
	st.put(&capture{Key: "/api/v2/app/config", MIME: "application/json", Body: []byte(`{"v":1}`)})
	st.put(&capture{Key: "/api/v2/detections/recent?limit=5", MIME: "application/json",
		Body: []byte(`[{"id":7,"clipName":"x.flac","source":{"id":"rtsp://u:p@nvr/yard?audio"}}]`)})
	st.put(&capture{Key: "/api/v2/detections/7", MIME: "application/json", Body: []byte(`{"v":1}`)}) // same body as config
	st.put(&capture{Key: "/api/v2/audio/7", MIME: "audio/flac", Body: []byte("fLaC....")})
	for k, v := range extra {
		st.put(&capture{Key: k, MIME: "application/json", Body: []byte(v)})
	}
	if _, err := buildSite(c, st, time.Now()); err != nil {
		t.Fatal(err)
	}
	return c, dir
}

func readJSON(t *testing.T, p string, v any) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
}

func TestBuildSite(t *testing.T) {
	_, dir := buildTestSite(t, nil)
	for _, f := range []string{"ui/index.html", "404.html", "_redirects", "robots.txt", "index.html", "sw.js",
		"_snapshot/boot.js", "_snapshot/snapshot.css", "ui/assets/index-abc.js", "snapshot.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	var info snapshotInfo
	readJSON(t, filepath.Join(dir, "snapshot.json"), &info)
	if info.Entries != 4 || info.Label != "Test snapshot" || info.Pages != 1 || info.ServerToday == "" {
		t.Errorf("bad snapshot.json: %+v", info)
	}
	var m manifest
	readJSON(t, filepath.Join(dir, strings.TrimPrefix(info.Manifest, "/")), &m)
	if len(m.Entries) != 4 {
		t.Fatalf("want 4 entries, got %v", m.Entries)
	}
	if m.Entries["/api/v2/app/config"].F != m.Entries["/api/v2/detections/7"].F {
		t.Error("identical bodies should share one content-addressed file")
	}
	if e := m.Entries["/api/v2/audio/7"]; !strings.HasSuffix(e.F, ".flac") || e.T != "" {
		t.Errorf("bad audio entry %+v", e)
	}
	rec, _ := os.ReadFile(filepath.Join(dir, "_snap", m.Entries["/api/v2/detections/recent?limit=5"].F))
	if strings.Contains(string(rec), "rtsp") {
		t.Errorf("stream URL published: %s", rec)
	}
	listed := map[string]bool{}
	for _, f := range m.Files {
		listed[f] = true
	}
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			if !listed[filepath.ToSlash(rel)] {
				t.Errorf("%s is published but not listed in the manifest", rel)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	robots, _ := os.ReadFile(filepath.Join(dir, "robots.txt"))
	if !strings.Contains(string(robots), "Disallow: /\n") {
		t.Errorf("default robots.txt should disallow all: %s", robots)
	}
}

func TestBuildSiteReplacesOldOutput(t *testing.T) {
	c, dir := buildTestSite(t, nil)
	stale := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := newTestStore()
	st.index = []byte(testShell)
	st.put(&capture{Key: "/api/v2/app/config", MIME: "application/json", Body: []byte(`{}`)})
	if _, err := buildSite(c, st, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("old output not replaced")
	}
	if _, err := os.Stat(dir + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp dir left behind")
	}
}

func TestServeHandler(t *testing.T) {
	_, dir := buildTestSite(t, nil)
	srv := httptest.NewServer(serveHandler(dir))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cases := []struct{ path, want string }{
		{"/ui/dashboard?date=2026-01-01", "boot.js"},
		{"/ui/detections/7", "boot.js"},
		{"/ui/index.html", "boot.js"},
		{"/", "url=/ui/dashboard"},
		{"/snapshot.json", `"manifest"`},
		{"/ui/assets/index-abc.js?v=2", "console.log(1)"},
	}
	for _, tc := range cases {
		resp, err := client.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		b := new(strings.Builder)
		_, _ = io.Copy(b, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(b.String(), tc.want) {
			t.Errorf("%s: status %d, body %.80q, want %q", tc.path, resp.StatusCode, b.String(), tc.want)
		}
	}
}

func TestMetrics(t *testing.T) {
	p := filepath.Join(t.TempDir(), "birdnet_snapshot.prom")
	r := &runReport{Start: time.Now().Add(-time.Minute), Build: buildResult{Pages: 3, Entries: 40, Clips: 2, Bytes: 1000}, Deploy: deployResult{Uploaded: 5}}
	if err := writeMetrics(p, r); err != nil {
		t.Fatal(err)
	}
	ok := readGauge(p, "birdnet_snapshot_last_success_timestamp_seconds")
	if ok == 0 || readGauge(p, "birdnet_snapshot_last_run_success") != 1 || readGauge(p, "birdnet_snapshot_entries") != 40 {
		b, _ := os.ReadFile(p)
		t.Fatalf("bad metrics:\n%s", b)
	}
	r2 := &runReport{Start: time.Now(), Err: os.ErrNotExist}
	if err := writeMetrics(p, r2); err != nil {
		t.Fatal(err)
	}
	if readGauge(p, "birdnet_snapshot_last_run_success") != 0 {
		t.Error("failed run should report success 0")
	}
	if readGauge(p, "birdnet_snapshot_last_success_timestamp_seconds") != ok {
		t.Error("last success time must survive a failed run")
	}
	b, _ := os.ReadFile(p)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasPrefix(line, "#") && !regexp.MustCompile(`^[a-z_]+(\{[^}]*\})? [-0-9.e+]+$`).MatchString(line) {
			t.Errorf("not a valid sample line: %q", line)
		}
	}
}

func TestSplitListAndEnv(t *testing.T) {
	if got := splitList(" a, ,b ,"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitList = %v", got)
	}
	t.Setenv("BNS_TEST_INT", "42")
	t.Setenv("BNS_TEST_BOOL", "true")
	t.Setenv("BNS_TEST_DUR", "90s")
	t.Setenv("BNS_TEST_BAD", "nope")
	if envInt("BNS_TEST_INT", 1) != 42 || envInt("BNS_TEST_BAD", 1) != 1 {
		t.Error("envInt")
	}
	if !envBool("BNS_TEST_BOOL", false) || envBool("BNS_TEST_BAD", false) {
		t.Error("envBool")
	}
	if envDur("BNS_TEST_DUR", 0) != 90*time.Second || envDur("BNS_TEST_BAD", time.Second) != time.Second {
		t.Error("envDur")
	}
}

func TestParseFlagsEnvFallback(t *testing.T) {
	t.Setenv("BIRDNET_URL", "http://bird.lan:8080/")
	t.Setenv("SKIP_AUDIO_SOURCES", "Doorbell, Porch")
	t.Setenv("SNAPSHOT_DAYS", "3")
	t.Setenv("BUNNY_STORAGE_KEY", "k")
	c, _, _ := parseFlags("crawl", []string{"-max-pages", "9", "-routes", "/ui/analytics/advanced,ui/x"})
	if c.BirdNET != "http://bird.lan:8080" || c.Days != 3 || c.MaxPages != 9 || c.ZoneKey != "k" {
		t.Errorf("bad config %+v", c)
	}
	if strings.Join(c.SkipAudio, ",") != "doorbell,porch" || len(c.Routes) != 2 {
		t.Errorf("bad lists %v %v", c.SkipAudio, c.Routes)
	}
}

func TestPurgeAudio(t *testing.T) {
	st := newTestStore()
	for _, k := range []string{"/api/v2/audio/1", "/api/v2/audio/2?t=5", "/api/v2/audio/3", "/api/v2/audio/x"} {
		st.put(&capture{Key: k, MIME: "audio/flac", Body: []byte("x")})
	}
	st.put(&capture{Key: "/api/v2/spectrogram/2?size=md&raw=true", MIME: "image/png", Body: []byte("p")})
	st.put(&capture{Key: "/api/v2/spectrogram/1?size=lg", MIME: "image/png", Body: []byte("p")})
	st.put(&capture{Key: "/api/v2/detections/recent", MIME: "application/json",
		Body: []byte(`[{"id":1,"clipName":"a.flac"},{"id":2,"clipName":"b.flac","nested":{"id":2,"clipName":"b.flac"}}]`)})
	dets := map[int64]detection{
		1: {ID: 1, ClipName: "a", Source: "rtsp yard"},
		2: {ID: 2, ClipName: "b", Source: "rtsp doorbell"},
		// 3 unknown
	}
	purgeAudio(config{SkipAudio: []string{"doorbell"}}, st, dets)
	if !st.has("/api/v2/audio/1") || !st.has("/api/v2/spectrogram/1?size=lg") || len(st.items) != 3 {
		t.Errorf("want only detection 1's media and the list kept, got %v", sortedKeys(st.items))
	}
	if got := string(st.items["/api/v2/detections/recent"].Body); got != `[{"clipName":"a.flac","id":1},{"clipName":"","id":2,"nested":{"clipName":"","id":2}}]` {
		t.Errorf("clipName of the withheld detection not blanked: %s", got)
	}
	purgeAudio(config{NoAudio: true}, st, dets)
	if len(st.items) != 1 {
		t.Errorf("NoAudio must drop every clip and spectrogram, got %v", sortedKeys(st.items))
	}
}

func TestServerZone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"timestamp":"2026-10-24T09:32:25-07:00"}]`))
	}))
	defer srv.Close()

	t.Setenv("TZ", "")
	off, ok := detectOffset(context.Background(), srv.URL)
	if !ok || off != -7*3600 {
		t.Fatalf("detectOffset = %d %v", off, ok)
	}
	loc, name := serverZone(context.Background(), srv.URL)
	_, localOff := time.Now().Zone()
	if localOff != -7*3600 {
		if _, o := time.Now().In(loc).Zone(); o != -7*3600 || name != "Etc/GMT+7" {
			t.Errorf("want UTC-7 / Etc/GMT+7, got %d %q", o, name)
		}
	}

	t.Setenv("TZ", "Asia/Kolkata")
	loc, name = serverZone(context.Background(), srv.URL)
	if loc.String() != "Asia/Kolkata" || name != "Asia/Kolkata" {
		t.Errorf("TZ must win: %s %q", loc, name)
	}
	t.Setenv("TZ", "Not/AZone")
	if loc, _ := serverZone(context.Background(), "http://127.0.0.1:1"); loc != time.Local {
		t.Errorf("bad TZ and no server should fall back to Local, got %s", loc)
	}
}

func TestRetryMedia(t *testing.T) {
	var (
		mu   sync.Mutex
		hits []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.URL.RequestURI())
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/api/v2/audio/9") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer srv.Close()
	st := newTestStore()
	st.put(&capture{Key: "/api/v2/spectrogram/5/status?size=md&raw=true", MIME: "application/json", Body: []byte(`{}`)})
	st.put(&capture{Key: "/api/v2/spectrogram/6/status?size=md&raw=true", MIME: "application/json", Body: []byte(`{}`)})
	st.put(&capture{Key: "/api/v2/spectrogram/6?size=md&raw=true", MIME: "image/png", Body: []byte("have")})
	st.misses = []string{"404 /api/v2/spectrogram/7?size=lg", "404 /api/v2/audio/9", "500 /api/v2/detections/3"}
	if err := retryMedia(context.Background(), config{BirdNET: srv.URL}, st); err != nil {
		t.Fatal(err)
	}
	if !st.has("/api/v2/spectrogram/5?size=md&raw=true") || !st.has("/api/v2/spectrogram/7?size=lg") {
		t.Errorf("missing spectrograms not recovered: %v", sortedKeys(st.items))
	}
	if st.has("/api/v2/audio/9") {
		t.Error("a 404 must not be stored")
	}
	for _, h := range hits {
		if strings.Contains(h, "spectrogram/6") || strings.Contains(h, "detections") {
			t.Errorf("unexpected request %s", h)
		}
	}
}

func TestRedactor(t *testing.T) {
	body := []byte(`{"rarity":{"score":0.9,"latitude":34.0312,"longitude":-118.4712,"location_based":true},
		"list":[{"lat":51.5,"lng":-0.12,"name":"x"}],"note":"call home.example.lan","label":"latitude"}`)
	c := &capture{MIME: "application/json", Body: body}

	hide, _ := newRedactor(config{Location: "hide", Redact: []string{`home\.example\.lan`}})
	out := string(hide.apply(c))
	for _, bad := range []string{"34.03", "118.47", "51.5", "-0.12", "home.example.lan"} {
		if strings.Contains(out, bad) {
			t.Errorf("hide left %q in %s", bad, out)
		}
	}
	if !strings.Contains(out, `"location_based":true`) || !strings.Contains(out, `"label":"latitude"`) || !strings.Contains(out, "redacted") {
		t.Errorf("hide removed too much: %s", out)
	}

	coarse, _ := newRedactor(config{Location: "coarse"})
	out = string(coarse.apply(c))
	if !strings.Contains(out, `"latitude":34`) || !strings.Contains(out, `"longitude":-118`) || strings.Contains(out, "34.03") {
		t.Errorf("coarse: %s", out)
	}

	keep, _ := newRedactor(config{Location: "keep"})
	if !strings.Contains(string(keep.apply(c)), "34.0312") {
		t.Error("keep must not touch coordinates")
	}
	plain := &capture{MIME: "application/json", Body: []byte(`{"a":1}`)}
	if string(hide.apply(plain)) != `{"a":1}` {
		t.Error("bodies without coordinates must be byte-identical (content addressing)")
	}
	if _, err := newRedactor(config{Location: "blur"}); err == nil {
		t.Error("bad -location must be rejected")
	}
	if _, err := newRedactor(config{Forbid: []string{"("}}); err == nil {
		t.Error("bad -forbid regex must be rejected")
	}
}

func TestForbidFailsBuild(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "site")
	st := newTestStore()
	st.index = []byte(testShell)
	st.put(&capture{Key: "/api/v2/app/config", MIME: "application/json", Body: []byte(`{"host":"birdnet.home.lan"}`)})
	_, err := buildSite(config{Out: dir, Forbid: []string{`home\.lan`}}, st, time.Now())
	if err == nil || !strings.Contains(err.Error(), "forbidden pattern") {
		t.Fatalf("want forbidden pattern error, got %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("nothing may be published when a forbidden pattern is found")
	}
}

func TestShellCSP(t *testing.T) {
	src := strings.Replace(testShell, "<title>", "<script>window.__inline = 1;</script><title>", 1)
	out, err := shell([]byte(src), false, true)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	m := regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]+)"`).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no CSP meta:\n%s", s)
	}
	csp := strings.ReplaceAll(m[1], "&#39;", "'")
	sum := sha256.Sum256([]byte("window.__inline = 1;"))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if !strings.Contains(csp, want) {
		t.Errorf("inline script hash %s missing from %s", want, csp)
	}
	for _, d := range []string{"default-src 'self'", "connect-src 'self'", "object-src 'none'", "base-uri 'none'", "form-action 'none'"} {
		if !strings.Contains(csp, d) {
			t.Errorf("CSP lacks %q", d)
		}
	}
	if strings.Contains(csp, "unsafe-inline' 'sha") || strings.Contains(strings.Split(csp, "style-src")[0], "unsafe-inline") {
		t.Errorf("scripts must not allow unsafe-inline: %s", csp)
	}
	if i := strings.Index(s, "Content-Security-Policy"); i > strings.Index(s, "<script") {
		t.Error("CSP meta must come before every script")
	}
	if out, _ := shell([]byte(src), false, false); strings.Contains(string(out), "Content-Security-Policy") {
		t.Error("-no-csp must drop the meta")
	}
}

func TestAssetClosure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ui/assets/Lazy-1.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`import("./Deeper-2.js")`))
		case "/ui/assets/Deeper-2.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`1`))
		case "/ui/assets/font.woff2":
			w.Header().Set("Content-Type", "font/woff2")
			_, _ = w.Write([]byte(`wOF2`))
		case "/ui/assets/bird-placeholder.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg/>`))
		default: // SPA fallback, like BirdNET-Go
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html>`))
		}
	}))
	defer srv.Close()
	st := newTestStore()
	st.put(&capture{Key: "/ui/assets/index-a.js", MIME: "text/javascript",
		Body: []byte(`const a=()=>import("./Lazy-1.js");const p="/ui/assets/bird-placeholder.svg";const m="./Missing-9.js";const x="https://cdn.example/x.js"`)})
	st.put(&capture{Key: "/ui/assets/index-a.css", MIME: "text/css", Body: []byte(`@font-face{src:url(./font.woff2)}.x{background:url(data:image/png;base64,AA)}`)})
	if err := fetchAssetClosure(context.Background(), config{BirdNET: srv.URL}, st); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"/ui/assets/Lazy-1.js", "/ui/assets/Deeper-2.js", "/ui/assets/font.woff2", "/ui/assets/bird-placeholder.svg"} {
		if !st.has(k) {
			t.Errorf("closure missed %s (have %v)", k, sortedKeys(st.items))
		}
	}
	if st.has("/ui/assets/Missing-9.js") {
		t.Error("the HTML fallback must not be stored as a chunk")
	}
}

func TestSnapshotListsProtected(t *testing.T) {
	_, dir := buildTestSite(t, nil)
	var info snapshotInfo
	readJSON(t, filepath.Join(dir, "snapshot.json"), &info)
	has := map[string]bool{}
	for _, p := range info.Protected {
		has[p] = true
	}
	if !has["/api/v2/notifications"] || !has["/api/v2/settings"] {
		t.Errorf("protected list %v", info.Protected)
	}
}

// Every extension extFor produces (except .bin, whose type is stored) must
// have a content type in the service worker's TYPES table.
func TestServiceWorkerKnowsEveryExtension(t *testing.T) {
	sw, _ := webFS.ReadFile("web/sw.js")
	m := regexp.MustCompile(`(?s)const TYPES = \{(.*?)\};`).FindSubmatch(sw)
	if m == nil {
		t.Fatal("TYPES not found in web/sw.js")
	}
	known := map[string]bool{}
	for _, km := range regexp.MustCompile(`(\w+):\s*'`).FindAllSubmatch(m[1], -1) {
		known["."+string(km[1])] = true
	}
	for _, mime := range []string{"application/json", "image/webp", "image/png", "image/jpeg", "image/svg+xml", "image/avif",
		"image/gif", "audio/flac", "audio/mpeg", "audio/wav", "audio/ogg", "audio/mp4", "text/plain", "text/css", "text/html", "text/javascript"} {
		if ext := extFor(mime); ext != ".bin" && !known[ext] {
			t.Errorf("extFor(%q) = %s, missing from sw.js TYPES", mime, ext)
		}
	}
}
