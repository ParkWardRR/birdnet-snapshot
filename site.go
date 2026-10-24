package main

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed web/*
var webFS embed.FS

// manifestEntry points a recorded URL at its content-addressed body.
type manifestEntry struct {
	F string `json:"f"`           // file name under /_snap/
	T string `json:"t,omitempty"` // content type, only when the extension does not imply it (sw.js TYPES)
}

type manifest struct {
	GeneratedAt int64                    `json:"generatedAt"`
	Entries     map[string]manifestEntry `json:"entries"`
	Files       []string                 `json:"files"` // every published path, for cleanup
}

// snapshotInfo is /snapshot.json, the only file the service worker re-reads.
type snapshotInfo struct {
	Manifest    string   `json:"manifest"`
	GeneratedAt int64    `json:"generatedAt"` // epoch ms
	ServerTZ    int      `json:"serverTZ"`    // JS getTimezoneOffset of the server
	ServerToday string   `json:"serverToday"` // YYYY-MM-DD in the server's zone
	Pages       int      `json:"pages"`
	Entries     int      `json:"entries"`
	Label       string   `json:"label"`              // text of the snapshot pill
	Version     string   `json:"version"`            // birdnet-snapshot version that built it
	Protected   []string `json:"protected"`          // API prefixes the service worker answers with 401, like a server does for guests
	Previous    string   `json:"previous,omitempty"` // manifest of the snapshot this one replaced, set by deploy
}

// buildResult summarises a build for logs and metrics.
type buildResult struct {
	Pages         int
	Entries       int
	Files         int
	Bytes         int64
	Clips         int
	Detections    int
	CrawlDuration time.Duration
	Duration      time.Duration
}

var (
	// rtsp://user:pass@nvr.lan:8554/garage?audio -> stream/garage
	rtspRe     = regexp.MustCompile(`rtsps?://[^"/\s]*/([^"?\s\\]*)(\?[^"\s\\]*)?`)
	swRegRe    = regexp.MustCompile(`(?s)<script>\s*if \('serviceWorker' in navigator\).*?</script>`)
	entryRe    = regexp.MustCompile(`<script type="module" crossorigin src="([^"]+)"></script>`)
	headOpenRe = regexp.MustCompile(`<head>`)
)

// sanitize scrubs recorded JSON with the default redactor: stream URLs (which
// can carry hostnames and credentials) keep only their last path segment and
// coordinates are removed.
func sanitize(c *capture) []byte {
	return (&redactor{location: locationHide}).apply(c)
}

func buildSite(c config, st *store, started time.Time) (*buildResult, error) {
	// Stamp with the build time, not the crawl start: detections recorded during
	// the crawl must not read as "in 2 minutes" on the pinned clock. The
	// dashboard day stays the crawl's first day, which is what it recorded.
	built := time.Now()
	res := &buildResult{Pages: len(st.visited)}
	tmp := c.Out + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	red, err := newRedactor(c)
	if err != nil {
		return nil, err
	}
	m := manifest{GeneratedAt: built.UnixMilli(), Entries: map[string]manifestEntry{}}
	write := func(rel string, b []byte) error {
		if err := red.check(rel, b); err != nil {
			return err
		}
		rel = path.Clean("/" + rel)
		dst := filepath.Join(tmp, filepath.FromSlash(rel))
		if !strings.HasPrefix(dst, filepath.Clean(tmp)+string(os.PathSeparator)) {
			return fmt.Errorf("refusing path %q", rel)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		m.Files = append(m.Files, strings.TrimPrefix(rel, "/"))
		res.Files++
		res.Bytes += int64(len(b))
		return os.WriteFile(dst, b, 0o644)
	}

	written := map[string]bool{}
	var snapBytes, staticBytes int
	for _, k := range sortedKeys(st.items) {
		it := st.items[k]
		p, q, _ := strings.Cut(k, "?")
		body := red.apply(it)
		if q == "" && !strings.HasPrefix(p, "/api/") {
			if strings.HasSuffix(p, "/") || p == "/index.html" || p == "/ui/index.html" {
				continue
			}
			// Keys are escaped like URL.pathname; files are stored by name.
			if up, err := url.PathUnescape(p); err == nil {
				p = up
			}
			if err := write(p, body); err != nil {
				return nil, err
			}
			staticBytes += len(body)
			continue
		}
		if statusRe.MatchString(k) {
			continue // answered by the service worker from what is recorded
		}
		if strings.HasPrefix(p, "/api/v2/audio/") {
			res.Clips++
		}
		sum := sha256.Sum256(body)
		ext := extFor(it.MIME)
		name := hex.EncodeToString(sum[:10]) + ext // 80 bits: no collisions at this scale, smaller manifest
		e := manifestEntry{F: name}
		if ext == ".bin" {
			e.T = it.MIME
		}
		m.Entries[k] = e
		if !written[name] {
			written[name] = true
			if err := write("_snap/"+name, body); err != nil {
				return nil, err
			}
			snapBytes += len(body)
		}
	}
	res.Entries = len(m.Entries)

	idx, err := shell(st.index, c.AllowIndex, !c.NoCSP)
	if err != nil {
		return nil, err
	}
	// ui/index.html serves every SPA route; 404.html and _redirects let static
	// hosts other than Bunny (GitHub/Cloudflare Pages, Netlify) do the same.
	for _, f := range []string{"ui/index.html", "404.html"} {
		if err := write(f, idx); err != nil {
			return nil, err
		}
	}
	if err := write("_redirects", []byte("/ui/*  /ui/index.html  200\n")); err != nil {
		return nil, err
	}
	if err := write("index.html", []byte(`<!doctype html><meta http-equiv="refresh" content="0; url=/ui/dashboard"><a href="/ui/dashboard">BirdNET-Go</a>`)); err != nil {
		return nil, err
	}
	robots := "User-agent: *\nDisallow: /\n"
	if c.AllowIndex {
		robots = "User-agent: *\nDisallow: /api/\nDisallow: /_snap/\n"
	}
	if err := write("robots.txt", []byte(robots)); err != nil {
		return nil, err
	}
	for _, f := range []struct{ src, dst string }{
		{"web/sw.js", "sw.js"},
		{"web/boot.js", "_snapshot/boot.js"},
		{"web/snapshot.css", "_snapshot/snapshot.css"},
	} {
		b, err := webFS.ReadFile(f.src)
		if err != nil {
			return nil, err
		}
		if err := write(f.dst, b); err != nil {
			return nil, err
		}
	}

	// Manifest last, so it lists every file including itself.
	sort.Strings(m.Files)
	mb, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	ms := sha256.Sum256(mb)
	mname := "_snap/manifest-" + hex.EncodeToString(ms[:8]) + ".json"
	m.Files = append(m.Files, mname, "snapshot.json")
	if mb, err = json.Marshal(m); err != nil {
		return nil, err
	}
	if err := write(mname, mb); err != nil {
		return nil, err
	}
	info := snapshotInfo{
		Manifest:    "/" + mname,
		GeneratedAt: built.UnixMilli(),
		ServerTZ:    st.serverTZ,
		ServerToday: started.In(st.zone()).Format("2006-01-02"),
		Pages:       len(st.visited),
		Entries:     len(m.Entries),
		Label:       c.Label,
		Version:     version,
		Protected:   protectedAPI(),
	}
	ib, _ := json.MarshalIndent(info, "", "  ")
	if err := write("snapshot.json", ib); err != nil {
		return nil, err
	}

	if err := os.RemoveAll(c.Out); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, c.Out); err != nil {
		return nil, err
	}

	res.Detections = len(st.detections())
	log.Printf("site: %d pages, %d API/query entries (%d files, %.1f MB), static %.1f MB",
		len(st.visited), len(m.Entries), len(written), mb2(snapBytes), mb2(staticBytes))
	if len(st.misses) > 0 {
		misses := uniqCounts(st.misses)
		log.Printf("non-200 API responses during crawl (%d distinct): %s", len(misses), strings.Join(firstN(misses, 15), "; "))
	}
	if len(st.blocked) > 0 {
		log.Printf("blocked during crawl: %s", strings.Join(firstN(sortedKeys(st.blocked), 30), ", "))
	}
	return res, nil
}

// shell turns the recorded SPA index into the snapshot's: the app's own
// service worker registration is dropped, the module entry is started by
// boot.js once our worker controls the page, indexing is disabled unless
// allowed, and a Content-Security-Policy allows only same-origin code plus
// the shell's own inline scripts (by hash).
func shell(index []byte, allowIndex, csp bool) ([]byte, error) {
	idx := swRegRe.ReplaceAll(index, nil)
	em := entryRe.FindSubmatch(idx)
	if em == nil {
		return nil, fmt.Errorf("SPA shell has no module entry script; BirdNET-Go UI changed?")
	}
	idx = entryRe.ReplaceAll(idx, nil)
	if !headOpenRe.Match(idx) {
		return nil, fmt.Errorf("SPA shell has no <head>")
	}
	var meta strings.Builder
	if csp {
		meta.WriteString("\n    <meta http-equiv=\"Content-Security-Policy\" content=\"" + html.EscapeString(contentSecurityPolicy(idx)) + "\" />")
	}
	if !allowIndex {
		meta.WriteString("\n    <meta name=\"robots\" content=\"noindex, nofollow\" />")
	}
	meta.WriteString("\n    <meta name=\"referrer\" content=\"no-referrer\" />")
	inject := fmt.Sprintf(`<head>%s
    <link rel="stylesheet" href="/_snapshot/snapshot.css" />
    <script src="/_snapshot/boot.js" data-entry="%s"></script>`, meta.String(), html.EscapeString(string(em[1])))
	return headOpenRe.ReplaceAll(idx, []byte(inject)), nil
}

var inlineScriptRe = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)

// contentSecurityPolicy allows same-origin scripts, the inline scripts of
// this shell by hash, and nothing from anywhere else: no third-party
// requests, frames, plugins or form posts.
func contentSecurityPolicy(shellHTML []byte) string {
	scripts := []string{"'self'"}
	for _, m := range inlineScriptRe.FindAllSubmatch(shellHTML, -1) {
		if strings.Contains(string(m[0][:strings.Index(string(m[0]), ">")]), "src=") || len(m[1]) == 0 {
			continue
		}
		sum := sha256.Sum256(m[1])
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"media-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"worker-src 'self'",
		"manifest-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'none'",
	}, "; ")
}

// protectedAPI lists the API prefixes a guest gets 401 for; the service
// worker answers them the same way so the UI stays quiet, as it does for a
// logged-out visitor of a real server.
func protectedAPI() []string {
	return append([]string(nil), deniedAPI...)
}

func extFor(mime string) string {
	mime, _, _ = strings.Cut(mime, ";")
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "application/json":
		return ".json"
	case "image/webp":
		return ".webp"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/aac", "audio/mp4":
		return ".m4a"
	case "text/plain":
		return ".txt"
	case "text/css":
		return ".css"
	case "text/html":
		return ".html"
	case "application/javascript", "text/javascript":
		return ".js"
	}
	return ".bin"
}

// uniqCounts collapses repeated lines into "line ×n".
func uniqCounts(lines []string) []string {
	n := map[string]int{}
	for _, l := range lines {
		n[l]++
	}
	out := make([]string, 0, len(n))
	for _, l := range sortedKeys(n) {
		if n[l] > 1 {
			out = append(out, fmt.Sprintf("%s ×%d", l, n[l]))
		} else {
			out = append(out, l)
		}
	}
	return out
}

func mb2(n int) float64 { return float64(n) / (1 << 20) }

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(s[:n:n], fmt.Sprintf("… +%d more", len(s)-n))
}
