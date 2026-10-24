// sitecheck loads a published snapshot in Chromium, Firefox and WebKit
// (Playwright) at desktop and phone sizes and reports everything a visitor
// would notice: console and page errors, failed requests, error toasts,
// broken images, service-worker control and load times. Exit status 1 when
// any problem is found.
//
//	go run . -url https://birds.example.com -shots /tmp/shots
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

type finding struct {
	Browser, Viewport, Page, Kind, Detail string
}

// Protected endpoints a guest gets 401 for, on the snapshot as on a real server.
var protected = []string{"/api/v2/settings", "/api/v2/system", "/api/v2/auth", "/api/v2/streams", "/api/v2/notifications", "/api/v2/alerts"}

// severity sorts a finding: "" (expected, dropped), "warn" (data the snapshot
// or the server does not have), or "fail" (something a visitor notices).
func severity(f finding) string {
	switch {
	case f.Kind == "HTTP 401":
		p := strings.TrimPrefix(f.Detail, "GET ")
		for _, pre := range protected {
			if strings.HasPrefix(p, pre) {
				return ""
			}
		}
		return "fail"
	case f.Kind == "console error" && strings.Contains(f.Detail, "status of 401"):
		return ""
	case f.Kind == "HTTP 403" && !strings.HasPrefix(f.Detail, "GET "):
		return "" // writes are refused by design
	case f.Kind == "console error" && strings.Contains(f.Detail, "status of 403"):
		return ""
	case f.Kind == "HTTP 404" && strings.Contains(f.Detail, " /api/"):
		return "warn"
	case f.Kind == "console error" && strings.Contains(f.Detail, "status of 404"):
		return "warn"
	}
	return "fail"
}

type timing struct {
	Browser, Viewport, Page string
	First, Repeat           time.Duration
}

func main() {
	site := flag.String("url", "http://127.0.0.1:8765", "snapshot base URL")
	browsers := flag.String("browsers", "chromium,firefox,webkit", "browsers to run")
	shots := flag.String("shots", "", "directory for screenshots (empty: none)")
	pagesFlag := flag.String("pages", "/ui/dashboard,/ui/analytics,/ui/analytics/species,/ui/analytics/trends,/ui/search,/ui/about,@detail", "pages; @detail = a recorded detection with audio")
	settle := flag.Duration("settle", 4*time.Second, "time to let a page settle after the app starts")
	strict := flag.Bool("strict", false, "fail on warnings too")
	ignore := flag.String("ignore", "", "comma-separated substrings of findings to ignore")
	flag.Parse()
	base := strings.TrimRight(*site, "/")

	pages := strings.Split(*pagesFlag, ",")
	for i, p := range pages {
		if p == "@detail" {
			id, err := detailID(base)
			if err != nil {
				log.Fatalf("pick a detection: %v", err)
			}
			pages[i] = "/ui/detections/" + id
		}
	}
	if *shots != "" {
		_ = os.MkdirAll(*shots, 0o755)
	}

	pw, err := playwright.Run()
	if err != nil {
		log.Fatalf("playwright: %v (install browsers: go run github.com/playwright-community/playwright-go/cmd/playwright install)", err)
	}
	defer pw.Stop()

	var (
		mu       sync.Mutex
		findings []finding
		timings  []timing
	)
	add := func(f finding) {
		for _, ig := range strings.Split(*ignore, ",") {
			if ig != "" && strings.Contains(f.Kind+" "+f.Detail, ig) {
				return
			}
		}
		mu.Lock()
		findings = append(findings, f)
		mu.Unlock()
	}

	for _, name := range strings.Split(*browsers, ",") {
		var bt playwright.BrowserType
		switch name {
		case "chromium":
			bt = pw.Chromium
		case "firefox":
			bt = pw.Firefox
		case "webkit":
			bt = pw.WebKit
		default:
			log.Fatalf("unknown browser %q", name)
		}
		browser, err := bt.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true), Timeout: playwright.Float(60000)})
		if err != nil {
			add(finding{name, "-", "-", "launch", err.Error()})
			continue
		}
		for _, vp := range []struct {
			name   string
			w, h   int
			mobile bool
		}{{"desktop", 1440, 900, false}, {"phone", 390, 844, true}} {
			opts := playwright.BrowserNewContextOptions{
				Viewport: &playwright.Size{Width: vp.w, Height: vp.h},
			}
			if vp.mobile && name != "firefox" { // Firefox has no mobile emulation
				opts.IsMobile = playwright.Bool(true)
				opts.HasTouch = playwright.Bool(true)
				opts.DeviceScaleFactor = playwright.Float(3)
			}
			ctx, err := browser.NewContext(opts)
			if err != nil {
				log.Fatalf("%s context: %v", name, err)
			}
			cur := ""
			label := func() (string, string, string) { return name, vp.name, cur }
			ctx.On("requestfailed", func(r playwright.Request) {
				b, v, p := label()
				f := r.Failure()
				msg := ""
				if f != nil {
					msg = f.Error()
				}
				if strings.Contains(msg, "ERR_ABORTED") || strings.Contains(msg, "NS_BINDING_ABORTED") || strings.Contains(msg, "cancelled") {
					return // navigations away abort pending requests; not a visitor problem
				}
				add(finding{b, v, p, "request failed", r.Method() + " " + strings.TrimPrefix(r.URL(), base) + " (" + msg + ")"})
			})
			ctx.On("response", func(r playwright.Response) {
				if r.Status() >= 400 {
					b, v, p := label()
					add(finding{b, v, p, fmt.Sprintf("HTTP %d", r.Status()), r.Request().Method() + " " + strings.TrimPrefix(r.URL(), base)})
				}
			})
			page, err := ctx.NewPage()
			if err != nil {
				log.Fatalf("%s page: %v", name, err)
			}
			page.On("console", func(m playwright.ConsoleMessage) {
				if m.Type() == "error" {
					b, v, p := label()
					add(finding{b, v, p, "console error", trunc(m.Text(), 300)})
				}
			})
			page.On("pageerror", func(e error) {
				b, v, p := label()
				add(finding{b, v, p, "page error", trunc(e.Error(), 300)})
			})

			for _, p := range pages {
				cur = p
				t := timing{Browser: name, Viewport: vp.name, Page: p}
				for pass := 0; pass < 2; pass++ { // first visit, then a repeat visit
					start := time.Now()
					if _, err := page.Goto(base+p, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
						add(finding{name, vp.name, p, "navigation", err.Error()})
						break
					}
					if err := page.Locator("#snapshot-banner").WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(30000)}); err != nil {
						add(finding{name, vp.name, p, "app did not start", "no snapshot banner within 30 s"})
						break
					}
					if pass == 0 {
						t.First = time.Since(start)
					} else {
						t.Repeat = time.Since(start)
					}
					// Let the page finish loading before navigating again: leaving
					// early aborts its requests, which the UI logs as errors.
					time.Sleep(*settle)
				}
				checkDOM(page, func(kind, detail string) { add(finding{name, vp.name, p, kind, detail}) })
				if *shots != "" {
					f := filepath.Join(*shots, fmt.Sprintf("%s-%s-%s.png", name, vp.name, slug(p)))
					_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(f), FullPage: playwright.Bool(false)})
				}
				mu.Lock()
				timings = append(timings, t)
				mu.Unlock()
			}
			_ = ctx.Close()
		}
		_ = browser.Close()
	}

	var fails, warns []finding
	for _, f := range findings {
		switch severity(f) {
		case "fail":
			fails = append(fails, f)
		case "warn":
			warns = append(warns, f)
		}
	}
	report(timings)
	list("warnings (not in the snapshot or not on the server)", warns)
	list("problems", fails)
	if len(fails) > 0 || (*strict && len(warns) > 0) {
		os.Exit(1)
	}
}

// checkDOM looks for what a visitor sees: toasts and alerts, images that
// failed to load, and whether the service worker controls the page.
func checkDOM(page playwright.Page, add func(kind, detail string)) {
	res, err := page.Evaluate(`() => {
		const out = {toasts: [], broken: [], controlled: !!(navigator.serviceWorker && navigator.serviceWorker.controller)};
		const sel = '[role="alert"], [role="status"], [data-sonner-toast], .toast, [class*="toast"], [class*="Toast"], .alert-error, [class*="notification"]';
		for (const el of document.querySelectorAll(sel)) {
			const t = (el.innerText || '').trim();
			const r = el.getBoundingClientRect();
			if (t && r.width > 0 && r.height > 0 && !el.closest('#snapshot-banner')) {
				const card = el.parentElement && el.parentElement.closest('article, li, section, [class*="card"]');
				const ctx = card ? card.innerText.replace(/\s+/g, ' ').slice(0, 120) : '';
				out.toasts.push(t.slice(0, 120) + (ctx ? ' — in: ' + ctx : ''));
			}
		}
		const checks = [];
		for (const img of document.images) {
			if (img.complete && img.naturalWidth === 0 && img.src && !img.src.startsWith('data:')) {
				const src = img.currentSrc || img.src;
				// Firefox reports width 0 for SVGs without intrinsic size; check the bytes instead.
				if (/\.svg(\?|$)/.test(src)) {
					checks.push(fetch(src).then(r => { if (!r.ok) out.broken.push(src); }, () => out.broken.push(src)));
				} else {
					out.broken.push(src);
				}
			}
		}
		return Promise.all(checks).then(() => out);
	}`)
	if err != nil {
		add("evaluate", err.Error())
		return
	}
	m, _ := res.(map[string]any)
	if c, _ := m["controlled"].(bool); !c {
		add("service worker", "page not controlled by the snapshot service worker")
	}
	seen := map[string]bool{}
	for _, t := range asStrings(m["toasts"]) {
		if !seen[t] && looksLikeError(t) {
			seen[t] = true
			add("toast/alert", t)
		}
	}
	for _, b := range asStrings(m["broken"]) {
		add("broken image", b)
	}
}

func looksLikeError(t string) bool {
	l := strings.ToLower(t)
	for _, w := range []string{"error", "failed", "fail", "unable", "could not", "couldn't", "not found", "unavailable", "denied", "forbidden", "network", "try again", "unauthorized"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func asStrings(v any) []string {
	var out []string
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// detailID picks the newest recorded detection that has a clip.
func detailID(base string) (string, error) {
	var info struct{ Manifest string }
	if err := getJSON(base+"/snapshot.json", &info); err != nil {
		return "", err
	}
	var m struct{ Entries map[string]any }
	if err := getJSON(base+info.Manifest, &m); err != nil {
		return "", err
	}
	best := ""
	for k := range m.Entries {
		if id, ok := strings.CutPrefix(k, "/api/v2/audio/"); ok {
			if _, has := m.Entries["/api/v2/detections/"+id]; has && (len(id) > len(best) || (len(id) == len(best) && id > best)) {
				best = id
			}
		}
	}
	if best == "" {
		return "", fmt.Errorf("no detection with both detail and audio in the manifest")
	}
	return best, nil
}

func getJSON(u string, v any) error {
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func report(timings []timing) {
	fmt.Println("\n== load times (to app start): first visit / repeat visit")
	for _, t := range timings {
		fmt.Printf("  %-8s %-7s %-34s %6s / %6s\n", t.Browser, t.Viewport, trunc(t.Page, 34), t.First.Round(10*time.Millisecond), t.Repeat.Round(10*time.Millisecond))
	}
}

func list(title string, findings []finding) {
	fmt.Printf("\n== %s: %d\n", title, len(findings))
	// Collapse identical findings across pages.
	type k struct{ Kind, Detail string }
	where := map[k][]string{}
	for _, f := range findings {
		key := k{f.Kind, f.Detail}
		where[key] = append(where[key], f.Browser+"/"+f.Viewport+" "+f.Page)
	}
	keys := make([]k, 0, len(where))
	for key := range where {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(where[keys[i]]) > len(where[keys[j]]) })
	for _, key := range keys {
		w := where[key]
		browsers := map[string]bool{}
		for _, x := range w {
			browsers[strings.Split(x, "/")[0]] = true
		}
		var bl []string
		for b := range browsers {
			bl = append(bl, b)
		}
		sort.Strings(bl)
		fmt.Printf("  ×%-3d [%s] %s: %s\n", len(w), strings.Join(bl, ","), key.Kind, key.Detail)
		if title == "problems" {
			for i, x := range w {
				if i == 3 {
					fmt.Printf("        … +%d more\n", len(w)-3)
					break
				}
				fmt.Printf("        at %s\n", x)
			}
		}
	}
}

func slug(p string) string {
	s := strings.NewReplacer("/", "_", "?", "_", "&", "_", "=", "-").Replace(strings.Trim(p, "/"))
	if s == "" {
		return "root"
	}
	return s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
