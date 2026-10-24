package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// capture is one recorded GET response, keyed by path plus raw query.
type capture struct {
	Key  string
	MIME string
	Body []byte
}

type store struct {
	mu        sync.Mutex
	items     map[string]*capture
	index     []byte   // the SPA shell, recorded from the first document load
	misses    []string // API URLs that answered non-200 during the crawl
	blocked   map[string]int
	visited   []string
	cur       string              // page being visited
	byPage    map[string][]string // keys recorded while on each page
	templates []string            // detail-page API URLs with {id}
	serverTZ  int                 // JS-style getTimezoneOffset of the server at crawl time
	loc       *time.Location      // the server's zone
}

func (s *store) put(c *capture) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[c.Key] = c
	s.byPage[s.cur] = append(s.byPage[s.cur], c.Key)
}

// detections returns every detection record found in the recorded JSON.
func (s *store) detections() map[int64]detection {
	s.mu.Lock()
	defer s.mu.Unlock()
	dets := map[int64]detection{}
	for k, it := range s.items {
		if !strings.HasPrefix(k, "/api/v2/") || !isJSON(it.MIME) {
			continue
		}
		var v any
		if json.Unmarshal(it.Body, &v) == nil {
			collectDetections(v, dets)
		}
	}
	return dets
}

func (s *store) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[key]
	return ok
}

// API prefixes that must never be published or even requested during the
// crawl: admin data, live streams and anything that could carry secrets.
var deniedAPI = []string{
	"/api/v2/settings", "/api/v2/system", "/api/v2/auth", "/api/v2/streams",
	"/api/v2/debug", "/api/v2/support", "/api/v2/integrations", "/api/v2/import",
	"/api/v2/notifications", "/api/v2/alerts", "/api/v2/control", "/api/v2/models",
	"/api/v2/backup", "/api/v2/logs", "/api/v2/events", "/api/v2/sse", "/api/v2/stream",
	"/api/v2/soundlevels", "/api/v2/detections/stream", "/api/v2/filesystem",
	"/api/v2/mqtt", "/api/v2/birdweather", "/api/v2/telemetry",
}

// UI routes the guest crawl follows.
var allowedUI = []string{
	"/ui/dashboard", "/ui/detections", "/ui/analytics", "/ui/search", "/ui/about", "/ui/species",
}

// Writes the crawl may pass through: rendering a spectrogram the UI asked
// for, exactly as a guest's browser would.
var allowedWrite = regexp.MustCompile(`^/api/v2/spectrogram/\d+/generate$`)

func deniedPath(p string) bool {
	if p == "/api/v2/settings/dashboard" {
		return false // display preferences only; served rewritten, see guestJSON
	}
	if p == "/sw.js" {
		return true
	}
	for _, d := range deniedAPI {
		if p == d || strings.HasPrefix(p, d+"/") || strings.HasPrefix(p, d+"?") {
			return true
		}
	}
	return false
}

func crawl(ctx context.Context, c config) (*store, error) {
	base, err := url.Parse(c.BirdNET)
	if err != nil {
		return nil, err
	}
	loc, tzName := serverZone(ctx, c.BirdNET)
	_, off := time.Now().In(loc).Zone()
	st := &store{items: map[string]*capture{}, blocked: map[string]int{}, byPage: map[string][]string{}, serverTZ: -off / 60, loc: loc}

	ctx, cancel := context.WithTimeout(ctx, c.Budget)
	defer cancel()

	var alloc context.Context
	var acancel context.CancelFunc
	if c.Chrome != "" {
		alloc, acancel = chromedp.NewRemoteAllocator(ctx, c.Chrome)
	} else {
		opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1440, 2400))
		if c.ChromePath != "" {
			opts = append(opts, chromedp.ExecPath(c.ChromePath))
		}
		if c.NoSandbox {
			opts = append(opts, chromedp.NoSandbox)
		}
		alloc, acancel = chromedp.NewExecAllocator(ctx, opts...)
	}
	defer acancel()
	tab, tcancel := chromedp.NewContext(alloc)
	defer tcancel()

	type reqInfo struct {
		url    string
		method string
		status int64
		mime   string
		doc    bool
		start  time.Time
	}
	var (
		mu       sync.Mutex
		reqs     = map[network.RequestID]*reqInfo{}
		inflight = map[network.RequestID]bool{}
		pending  atomic.Int64
		client   = &http.Client{Timeout: 30 * time.Second}
	)

	sameOrigin := func(u *url.URL) bool { return u.Scheme == base.Scheme && u.Host == base.Host }

	chromedp.ListenTarget(tab, func(ev any) {
		switch e := ev.(type) {
		case *fetch.EventRequestPaused:
			go func() {
				u, err := url.Parse(e.Request.URL)
				var act chromedp.Action = fetch.ContinueRequest(e.RequestID)
				switch {
				case err != nil || !sameOrigin(u):
					// third-party (e.g. species images on a CDN): let it load, not recorded
				case e.Request.Method == http.MethodPost && allowedWrite.MatchString(u.Path):
					// pass through
				case e.Request.Method != http.MethodGet && e.Request.Method != http.MethodHead:
					act = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient)
					st.mu.Lock()
					st.blocked[e.Request.Method+" "+u.Path]++
					st.mu.Unlock()
				case deniedPath(u.Path):
					act = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient)
					st.mu.Lock()
					st.blocked[u.Path]++
					st.mu.Unlock()
				case rewritten[u.Path]:
					body, err := guestJSON(client, base.ResolveReference(&url.URL{Path: u.Path}).String())
					if err != nil {
						log.Printf("%s: %v", u.Path, err)
						act = fetch.FailRequest(e.RequestID, network.ErrorReasonFailed)
						break
					}
					st.put(&capture{Key: key(u), MIME: "application/json", Body: body})
					act = fetch.FulfillRequest(e.RequestID, 200).
						WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "application/json"}}).
						WithBody(base64.StdEncoding.EncodeToString(body))
				}
				_ = chromedp.Run(tab, act)
			}()
		case *network.EventRequestWillBeSent:
			mu.Lock()
			reqs[e.RequestID] = &reqInfo{url: e.Request.URL, method: e.Request.Method, doc: e.Type == network.ResourceTypeDocument, start: time.Now()}
			inflight[e.RequestID] = true
			mu.Unlock()
		case *network.EventResponseReceived:
			mu.Lock()
			if r := reqs[e.RequestID]; r != nil {
				r.status, r.mime = e.Response.Status, e.Response.MimeType
			}
			mu.Unlock()
		case *network.EventLoadingFailed:
			mu.Lock()
			delete(inflight, e.RequestID)
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			r := reqs[e.RequestID]
			delete(inflight, e.RequestID)
			mu.Unlock()
			if r == nil || r.method != http.MethodGet {
				return
			}
			u, err := url.Parse(r.url)
			if err != nil || !sameOrigin(u) || deniedPath(u.Path) || rewritten[u.Path] {
				return
			}
			if r.status != 200 {
				if strings.HasPrefix(u.Path, "/api/") {
					st.mu.Lock()
					st.misses = append(st.misses, fmt.Sprintf("%d %s", r.status, key(u)))
					st.mu.Unlock()
				}
				return
			}
			pending.Add(1)
			go func() {
				defer pending.Add(-1)
				var body []byte
				err := chromedp.Run(tab, chromedp.ActionFunc(func(ctx context.Context) error {
					var err error
					body, err = network.GetResponseBody(e.RequestID).Do(ctx)
					return err
				}))
				if err != nil {
					return
				}
				if r.doc {
					st.mu.Lock()
					if st.index == nil && strings.HasPrefix(u.Path, "/ui/") {
						st.index = body
					}
					st.mu.Unlock()
					return
				}
				st.put(&capture{Key: key(u), MIME: r.mime, Body: body})
			}()
		}
	})

	if err := chromedp.Run(tab,
		network.Enable(),
		network.SetCacheDisabled(true),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		chromedp.EmulateViewport(1440, 2400),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if tzName == "" {
				return nil
			}
			return emulation.SetTimezoneOverride(tzName).Do(ctx)
		}),
	); err != nil {
		return nil, fmt.Errorf("start chrome: %w", err)
	}

	waitIdle := func(max time.Duration) {
		deadline := time.Now().Add(max)
		quietSince := time.Time{}
		for time.Now().Before(deadline) && ctx.Err() == nil {
			mu.Lock()
			// Requests open longer than 8 s are long-polls (spectrogram status): not
			// worth waiting for.
			n := 0
			for id := range inflight {
				if r := reqs[id]; r == nil || time.Since(r.start) < 8*time.Second {
					n++
				}
			}
			mu.Unlock()
			if n == 0 && pending.Load() == 0 {
				if quietSince.IsZero() {
					quietSince = time.Now()
				} else if time.Since(quietSince) > 1200*time.Millisecond {
					return
				}
			} else {
				quietSince = time.Time{}
			}
			time.Sleep(100 * time.Millisecond)
		}
		mu.Lock()
		var stuck []string
		for id := range inflight {
			if r := reqs[id]; r != nil {
				stuck = append(stuck, r.url)
			}
		}
		mu.Unlock()
		if len(stuck) > 0 {
			log.Printf("  not idle after %s: %s", max, strings.Join(stuck, ", "))
		}
	}

	// visit loads one page, scrolls lazy content into view and returns the
	// same-origin UI links on it.
	visit := func(page string) []string {
		st.mu.Lock()
		st.cur = page
		st.mu.Unlock()
		t0 := time.Now()
		if err := chromedp.Run(tab, chromedp.Navigate(base.String()+page)); err != nil {
			log.Printf("navigate %s: %v", page, err)
			return nil
		}
		waitIdle(c.PageWait)
		_ = chromedp.Run(tab, chromedp.Evaluate(`(() => {
			window.scrollTo(0, document.documentElement.scrollHeight);
			document.querySelectorAll('main, [class*="overflow-y-auto"], [class*="overflow-auto"]').forEach(e => e.scrollTop = e.scrollHeight);
		})()`, nil))
		waitIdle(c.PageWait / 2)
		var hrefs []string
		_ = chromedp.Run(tab, chromedp.Evaluate(`Array.from(document.querySelectorAll('a[href]'), a => a.href)`, &hrefs))
		var links []string
		for _, l := range hrefs {
			if u, err := url.Parse(l); err == nil && sameOrigin(u) && allowedRoute(u.Path) {
				links = append(links, key(u))
			}
		}
		st.visited = append(st.visited, page)
		log.Printf("page %3d %-72s %5s  %d captured", len(st.visited), trunc(page, 72),
			time.Since(t0).Round(100*time.Millisecond), len(st.items))
		return links
	}

	// Phase 1: the dashboard for each recorded day and the other guest pages.
	var seeds []string
	today := time.Now().In(loc)
	for d := 0; d < c.Days; d++ {
		seeds = append(seeds, "/ui/dashboard?date="+today.AddDate(0, 0, -d).Format("2006-01-02"))
	}
	seeds = append(seeds, "/ui/dashboard", "/ui/analytics", "/ui/analytics/species", "/ui/analytics/summary",
		"/ui/analytics/activity", "/ui/analytics/trends", "/ui/analytics/biodiversity", "/ui/analytics/nocturnal",
		"/ui/analytics/weather", "/ui/analytics/soundscape", "/ui/search", "/ui/about")
	for _, r := range c.Routes {
		if !strings.HasPrefix(r, "/") {
			r = "/" + r
		}
		seeds = append(seeds, r)
	}

	seen := map[string]bool{}
	queues := map[string][]string{}
	enqueue := func(links []string) {
		for _, l := range links {
			if !seen[l] {
				seen[l] = true
				cl := linkClass(l)
				queues[cl] = append(queues[cl], l)
			}
		}
	}
	for _, s := range seeds {
		seen[s] = true
	}
	for _, s := range seeds {
		enqueue(visit(s))
	}

	// Phase 2: detection detail pages are opened from script, not links. Visit
	// two and learn which API URLs they load, so the rest can be fetched directly.
	learnDetailTemplates(st, visit)

	// Phase 3: links found so far, most useful class first, each class capped.
	done := map[string]int{}
	for len(st.visited) < c.MaxPages && ctx.Err() == nil {
		page := ""
		for _, cl := range linkClasses {
			for len(queues[cl.name]) > 0 && done[cl.name] < cl.cap {
				page, queues[cl.name] = queues[cl.name][0], queues[cl.name][1:]
				done[cl.name]++
				break
			}
			if page != "" {
				break
			}
		}
		if page == "" {
			break
		}
		enqueue(visit(page))
	}
	left := 0
	for _, q := range queues {
		left += len(q)
	}
	log.Printf("links: visited %v, %d left unvisited", done, left)

	// Phase 4: the phone layout requests some data differently.
	if ctx.Err() == nil && !c.NoMobile {
		if err := chromedp.Run(tab, chromedp.EmulateViewport(390, 844, chromedp.EmulateScale(3), chromedp.EmulateMobile)); err == nil {
			for _, s := range seeds {
				if ctx.Err() != nil {
					break
				}
				visit(s)
			}
		}
	}

	if st.index == nil {
		return nil, fmt.Errorf("never recorded the SPA shell")
	}
	return st, nil
}

// Link classes in crawl priority order, with visit caps.
var linkClasses = []struct {
	name string
	cap  int
}{
	{"dashboard", 30},
	{"species", 150},     // all detections of a species on a day
	{"hourly", 60},       // one hour of the daily grid
	{"species-hour", 60}, // one species in one hour
	{"other", 40},
}

func linkClass(l string) string {
	p, q, _ := strings.Cut(l, "?")
	if p == "/ui/dashboard" {
		return "dashboard"
	}
	if p != "/ui/detections" {
		return "other"
	}
	v, _ := url.ParseQuery(q)
	switch {
	case v.Get("queryType") == "hourly":
		return "hourly"
	case v.Get("queryType") == "species" && v.Has("hour"):
		return "species-hour"
	case v.Get("queryType") == "species":
		return "species"
	}
	return "other"
}

// learnDetailTemplates visits the detail page of two recent detections and
// records, as templates with {id}, the API URLs those pages loaded.
func learnDetailTemplates(st *store, visit func(string) []string) {
	dets := st.detections()
	var ids []int64
	for id, d := range dets {
		if d.ClipName != "" {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > 2 {
		ids = ids[:2]
	}
	tpl := map[string]int{}
	for _, id := range ids {
		page := fmt.Sprintf("/ui/detections/%d", id)
		visit(page)
		re := regexp.MustCompile(`\b` + strconv.FormatInt(id, 10) + `\b`)
		st.mu.Lock()
		for _, k := range st.byPage[page] {
			if strings.HasPrefix(k, "/api/") && re.MatchString(k) && !strings.HasPrefix(k, "/api/v2/audio/") {
				tpl[re.ReplaceAllString(k, "{id}")]++
			}
		}
		st.mu.Unlock()
	}
	// The detail player's large spectrogram renders async and may miss the
	// sampled visits; a GET renders it on demand.
	tpl["/api/v2/spectrogram/{id}?size=lg"] = len(ids)
	for t, n := range tpl {
		if n == len(ids) { // seen on every sampled page, so it is not id-specific noise
			st.templates = append(st.templates, t)
		}
	}
	sort.Strings(st.templates)
	log.Printf("detail templates: %v", st.templates)
}

// Endpoints the crawl serves rewritten by guestJSON instead of as recorded.
var rewritten = map[string]bool{"/api/v2/app/config": true, "/api/v2/settings/dashboard": true}

// guestJSON fetches app/config or settings/dashboard and rewrites it to what
// an unauthenticated visitor of an auth-enabled server sees, minus live widgets.
func guestJSON(client *http.Client, u string) ([]byte, error) {
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if strings.HasSuffix(u, "/app/config") {
		cfg["csrfToken"] = ""
		cfg["security"] = map[string]any{
			"enabled":       true,
			"accessAllowed": false,
			"authConfig":    map[string]any{"basicEnabled": false, "enabledProviders": []string{}},
			"publicAccess":  map[string]any{"liveAudio": false},
			"privateMode":   false,
		}
		cfg["newVersion"] = false
		cfg["freshInstall"] = false
		cfg["audioExportEnabled"] = false
	}
	cfg["liveSpectrogram"] = false
	if layout, ok := cfg["layout"].(map[string]any); ok {
		if els, ok := layout["elements"].([]any); ok {
			var keep []any
			for _, el := range els {
				m, _ := el.(map[string]any)
				switch m["type"] {
				case "currently-hearing", "live-spectrogram":
					continue // need live SSE streams
				}
				keep = append(keep, el)
			}
			layout["elements"] = keep
		}
	}
	return json.Marshal(cfg)
}

// volatileParams are cache busters the UI appends; keys and the service worker
// (web/sw.js, keep in sync) drop them so a recorded response still matches.
var volatileParams = map[string]bool{"t": true, "cache": true, "retry": true}

// key is the lookup key of a URL: path plus raw query minus volatile params.
func key(u *url.URL) string {
	var keep []string
	for _, p := range strings.Split(u.RawQuery, "&") {
		name, _, _ := strings.Cut(p, "=")
		if p != "" && !volatileParams[name] {
			keep = append(keep, p)
		}
	}
	if len(keep) == 0 {
		return u.Path
	}
	return u.Path + "?" + strings.Join(keep, "&")
}

func allowedRoute(p string) bool {
	for _, a := range allowedUI {
		if p == a || strings.HasPrefix(p, a+"/") {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validate refuses to build from a crawl that is clearly broken, so a
// BirdNET-Go restart or outage never replaces a good published snapshot.
func validate(c config, st *store) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.index == nil {
		return fmt.Errorf("no SPA shell recorded: is %s a BirdNET-Go server?", c.BirdNET)
	}
	if _, ok := st.items["/api/v2/app/config"]; !ok {
		return fmt.Errorf("app/config was never loaded: the UI did not start")
	}
	api := 0
	for k := range st.items {
		if strings.HasPrefix(k, "/api/") {
			api++
		}
	}
	if api < c.MinEntries {
		return fmt.Errorf("only %d API responses recorded (minimum %d); not publishing", api, c.MinEntries)
	}
	return nil
}

// zone is the server's zone, or Local for stores built without a crawl.
func (s *store) zone() *time.Location {
	if s.loc == nil {
		return time.Local
	}
	return s.loc
}
