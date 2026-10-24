package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Files that switch visitors to the new snapshot; uploaded after everything
// they reference, with snapshot.json strictly last.
var swapOrder = []string{
	"sw.js", "_snapshot/boot.js", "_snapshot/snapshot.css",
	"ui/index.html", "404.html", "_redirects", "robots.txt", "index.html", "snapshot.json",
}

type bunny struct {
	base   string // https://host/zone/
	key    string
	client *http.Client
}

type remoteObject struct {
	ObjectName  string
	Path        string
	IsDirectory bool
	Checksum    string
}

// deployResult summarises a deploy for logs and metrics.
type deployResult struct {
	Uploaded int
	Deleted  int
	Bytes    int64
	Duration time.Duration
}

func newBunny(c config) (*bunny, error) {
	if c.Zone == "" || c.ZoneKey == "" {
		return nil, errors.New("set BUNNY_STORAGE_ZONE and BUNNY_STORAGE_KEY (the storage zone password)")
	}
	host := c.ZoneHost
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}
	return &bunny{
		base:   strings.TrimRight(host, "/") + "/" + c.Zone + "/",
		key:    c.ZoneKey,
		client: &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// deploy syncs c.Out to the storage zone: uploads files whose SHA-256 differs,
// swaps the entry files last, then deletes remote files that neither the new
// nor the previous snapshot references (one generation of grace for visitors
// still on the old manifest).
func deploy(ctx context.Context, c config) (*deployResult, error) {
	start := time.Now()
	b, err := newBunny(c)
	if err != nil {
		return nil, err
	}

	local := map[string]string{} // rel path -> sha256 hex upper
	sizes := map[string]int64{}
	err = filepath.WalkDir(c.Out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(c.Out, p)
		rel = filepath.ToSlash(rel)
		sum, err := fileSHA(p)
		local[rel] = sum
		if fi, ierr := d.Info(); ierr == nil {
			sizes[rel] = fi.Size()
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if _, ok := local["snapshot.json"]; !ok {
		return nil, fmt.Errorf("%s has no snapshot.json; run crawl first", c.Out)
	}
	var next snapshotInfo
	if raw, err := os.ReadFile(filepath.Join(c.Out, "snapshot.json")); err == nil {
		_ = json.Unmarshal(raw, &next)
	}

	remote, err := b.list(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list zone: %w", err)
	}
	keep := map[string]bool{}
	if prev, files, err := b.previous(ctx); err == nil {
		for _, f := range files {
			keep[f] = true
		}
		// Shrink guard: a crawl that lost most of its data (server restarting,
		// half the UI failing) must not replace a good snapshot.
		if c.MaxShrink > 0 && !c.Force && prev.Entries > 0 &&
			float64(next.Entries) < c.MaxShrink*float64(prev.Entries) {
			return nil, fmt.Errorf("new snapshot has %d entries, live one %d (below %.0f%%); not deploying (use -force to override)",
				next.Entries, prev.Entries, c.MaxShrink*100)
		}
	}

	swap := map[string]bool{}
	for _, f := range swapOrder {
		swap[f] = true
	}
	var first, last []string
	for rel, sum := range local {
		if remote[rel] == sum || swap[rel] {
			continue
		}
		first = append(first, rel)
	}
	sort.Strings(first)
	for _, rel := range swapOrder {
		if sum, ok := local[rel]; ok && remote[rel] != sum {
			last = append(last, rel)
		}
	}
	var stale []string
	for rel := range remote {
		if _, ok := local[rel]; !ok && !keep[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	res := &deployResult{Uploaded: len(first) + len(last), Deleted: len(stale)}
	for _, f := range append(first, last...) {
		res.Bytes += sizes[f]
	}
	log.Printf("deploy: %d local, %d remote; upload %d + %d entry files (%.1f MB), delete %d",
		len(local), len(remote), len(first), len(last), float64(res.Bytes)/(1<<20), len(stale))
	if c.DryRun {
		for _, f := range append(first, last...) {
			log.Printf("  put %s", f)
		}
		for _, f := range stale {
			log.Printf("  del %s", f)
		}
		return res, nil
	}

	if err := b.parallel(ctx, first, c.Concurrency, func(rel string) error { return b.put(ctx, c.Out, rel, local[rel]) }); err != nil {
		return nil, err
	}
	for _, rel := range last {
		if err := b.put(ctx, c.Out, rel, local[rel]); err != nil {
			return nil, err
		}
	}
	if err := b.parallel(ctx, stale, c.Concurrency, func(rel string) error { return b.del(ctx, rel) }); err != nil {
		log.Printf("cleanup: %v", err) // stale files are harmless; the next run retries
	}
	res.Duration = time.Since(start)
	log.Printf("deploy: done in %s", res.Duration.Round(time.Second))
	return res, nil
}

// list returns every file under dir as rel path -> checksum.
func (b *bunny) list(ctx context.Context, dir string) (map[string]string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.base+escapePath(dir), nil)
	req.Header.Set("AccessKey", b.key)
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return map[string]string{}, nil
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list %q: %s %s", dir, resp.Status, body)
	}
	var objs []remoteObject
	if err := json.NewDecoder(resp.Body).Decode(&objs); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, o := range objs {
		rel := dir + o.ObjectName
		if o.IsDirectory {
			sub, err := b.list(ctx, rel+"/")
			if err != nil {
				return nil, err
			}
			for k, v := range sub {
				out[k] = v
			}
			continue
		}
		out[rel] = strings.ToUpper(o.Checksum)
	}
	return out, nil
}

// previous reads the snapshot currently live and its file list.
func (b *bunny) previous(ctx context.Context) (snapshotInfo, []string, error) {
	var info snapshotInfo
	if err := b.getJSON(ctx, "snapshot.json", &info); err != nil {
		return info, nil, err
	}
	var m manifest
	if err := b.getJSON(ctx, strings.TrimPrefix(info.Manifest, "/"), &m); err != nil {
		return info, nil, err
	}
	return info, m.Files, nil
}

func (b *bunny) getJSON(ctx context.Context, rel string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.base+escapePath(rel), nil)
	req.Header.Set("AccessKey", b.key)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("get %s: %s", rel, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (b *bunny) put(ctx context.Context, root, rel, sum string) error {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPut, b.base+escapePath(rel), bytes.NewReader(data))
		req.Header.Set("AccessKey", b.key)
		req.Header.Set("Checksum", sum)
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := b.client.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
				return nil
			}
			err = fmt.Errorf("put %s: %s %s", rel, resp.Status, body)
		}
		last = err
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	return last
}

func (b *bunny) del(ctx context.Context, rel string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, b.base+escapePath(rel), nil)
	req.Header.Set("AccessKey", b.key)
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete %s: %s", rel, resp.Status)
	}
	return nil
}

func (b *bunny) parallel(ctx context.Context, items []string, n int, f func(string) error) error {
	var (
		wg   sync.WaitGroup
		sem  = make(chan struct{}, n)
		mu   sync.Mutex
		errs []error
	)
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := f(it); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(append(errs, ctx.Err())...)
}

func escapePath(rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}
