package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// Vite chunk imports and asset URLs inside built JS: "./Foo-a1b2.js",
	// "/ui/assets/bird-placeholder.svg", "assets/x.woff2".
	jsRefRe = regexp.MustCompile(`["'` + "`" + `](\.{1,2}/[A-Za-z0-9_.@\-/]+\.(?:js|mjs|css|svg|png|webp|jpg|jpeg|gif|ico|woff2?|ttf|json|wasm|mp3|wav|ogg|m4a|webmanifest)|/ui/assets/[A-Za-z0-9_.@\-/]+\.(?:js|mjs|css|svg|png|webp|jpg|jpeg|gif|ico|woff2?|ttf|json|wasm|mp3|wav|ogg|m4a|webmanifest))["'` + "`" + `]`)
	// url(...) in CSS.
	cssRefRe = regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
	// href="/ui/..." and src="/ui/..." in the SPA shell (manifest, icons).
	htmlRefRe = regexp.MustCompile(`(?:href|src)="(/ui/[^"#?]+)"`)
)

const maxAssetFetches = 3000

// fetchAssetClosure records every chunk, stylesheet and asset referenced by
// the recorded JS and CSS that the crawl did not load itself: lazy route
// chunks, the error-fallback component, icons used only in rare states,
// fonts. Without them a visitor who hits one of those code paths gets the SPA
// shell served as JavaScript.
func fetchAssetClosure(ctx context.Context, c config, st *store) error {
	client := &http.Client{Timeout: 30 * time.Second}
	seen := map[string]bool{}
	var queue []string
	scan := func(k string, it *capture) {
		p, _, _ := strings.Cut(k, "?")
		if !strings.HasPrefix(p, "/ui/") {
			return
		}
		var refs [][]string
		switch {
		case strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".mjs") || strings.Contains(it.MIME, "javascript"):
			refs = jsRefRe.FindAllStringSubmatch(string(it.Body), -1)
		case strings.HasSuffix(p, ".css") || strings.Contains(it.MIME, "css"):
			refs = cssRefRe.FindAllStringSubmatch(string(it.Body), -1)
		default:
			return
		}
		for _, m := range refs {
			ref := m[1]
			if strings.HasPrefix(ref, "data:") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "#") {
				continue
			}
			var abs string
			if strings.HasPrefix(ref, "/") {
				abs = path.Clean(ref)
			} else {
				abs = path.Join(path.Dir(p), ref)
			}
			if !strings.HasPrefix(abs, "/ui/") {
				continue
			}
			u := &url.URL{Path: abs}
			k := key(u)
			if !seen[k] {
				seen[k] = true
				queue = append(queue, k)
			}
		}
	}
	st.mu.Lock()
	for k := range st.items {
		seen[k] = true
	}
	for k, it := range st.items {
		scan(k, it)
	}
	for _, m := range htmlRefRe.FindAllSubmatch(st.index, -1) {
		if k := key(&url.URL{Path: string(m[1])}); !seen[k] {
			seen[k] = true
			queue = append(queue, k)
		}
	}
	st.mu.Unlock()

	var (
		mu      sync.Mutex
		fetched int
		failed  int
	)
	for round := 0; len(queue) > 0 && fetched+failed < maxAssetFetches; round++ {
		batch := queue
		queue = nil
		var wg sync.WaitGroup
		sem := make(chan struct{}, 6)
		for _, k := range batch {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				body, mime, err := get(ctx, client, c.BirdNET+k)
				mu.Lock()
				defer mu.Unlock()
				// The server's SPA fallback answers unknown /ui/ paths with the
				// HTML shell; that is not the asset.
				if err != nil || (strings.Contains(mime, "text/html") && !strings.HasSuffix(k, ".html")) {
					failed++
					return
				}
				fetched++
				it := &capture{Key: k, MIME: mime, Body: body}
				st.put(it)
				scan(k, it)
			}()
		}
		wg.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	if fetched+failed > 0 {
		log.Printf("assets: %d referenced files fetched that no page loaded (%d unavailable)", fetched, failed)
	}
	return nil
}
