package main

import (
	"context"
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
	"time"
)

// freshDetections is how many of the newest detections get a clip attempt
// even if their record had no clip name yet when it was recorded.
const freshDetections = 40

// detection is the part of a detection record needed to decide on its clip.
type detection struct {
	ID       int64
	ClipName string
	Source   string // source id + display name, lowercased
	Sci      string // scientific name
}

// fetchClips downloads the audio clip of every detection the crawl saw, since
// the UI only requests a clip when the visitor presses play. Clips from
// sources in c.SkipAudio (say, a doorbell mic that hears people) are never
// fetched, and none are with c.NoAudio.
func fetchClips(ctx context.Context, c config, st *store) error {
	dets := st.detections()
	if err := fetchDetails(ctx, c, st, dets); err != nil {
		return err
	}
	// The newest detections may have had no clip yet when the crawl recorded
	// them; by now (minutes later) BirdNET-Go has usually written it.
	newest := make([]int64, 0, len(dets))
	for id := range dets {
		newest = append(newest, id)
	}
	sort.Slice(newest, func(i, j int) bool { return newest[i] > newest[j] })
	if len(newest) > freshDetections {
		newest = newest[:freshDetections]
	}
	fresh := map[int64]bool{}
	for _, id := range newest {
		fresh[id] = true
	}

	ids := make([]int64, 0, len(dets))
	skipped := 0
	for id, d := range dets {
		if c.NoAudio || (d.ClipName == "" && !fresh[id]) || skipSource(d.Source, c.SkipAudio) {
			skipped++
			continue
		}
		ids = append(ids, id)
	}
	// Newest (highest id) first, so the budget keeps the recent clips.
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > c.MaxClips {
		ids = ids[:c.MaxClips]
	}

	client := &http.Client{Timeout: 60 * time.Second}
	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
		mu      sync.Mutex
		fetched int
		failed  int
	)
	for _, id := range ids {
		k := fmt.Sprintf("/api/v2/audio/%d", id)
		if st.has(k) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			body, mime, err := get(ctx, client, c.BirdNET+k)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return
			}
			fetched++
			st.put(&capture{Key: k, MIME: mime, Body: body})
		}()
	}
	wg.Wait()
	log.Printf("clips: %d detections, %d fetched, %d failed, %d skipped (no clip or private source)",
		len(dets), fetched, failed, skipped)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Last, so media that was still being written during the crawl has had
	// the most time to appear; then drop clips that must not be published.
	if err := retryMedia(ctx, c, st); err != nil {
		return err
	}
	purgeAudio(c, st, st.detections())
	return nil
}

// mediaRefRe matches recorded media of one detection: its clip and every
// spectrogram (any size) or spectrogram status.
var mediaRefRe = regexp.MustCompile(`^/api/v2/(audio|spectrogram)/([^/?]+)(/status)?(\?.*)?$`)

// purgeAudio withholds the recording of every detection whose clip must not
// be published (c.NoAudio, a source in c.SkipAudio, or an unknown source):
// its clip and its spectrograms (a picture of the same audio, which can be
// turned back into rough sound) are dropped, including any the browser
// preloaded, and its clipName is blanked in every recorded response so the UI
// shows the detection without a player instead of an error.
func purgeAudio(c config, st *store, dets map[int64]detection) {
	withheld := func(id int64) bool {
		d, known := dets[id]
		return c.NoAudio || !known || skipSource(d.Source, c.SkipAudio)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	removed, blanked := 0, 0
	for k, it := range st.items {
		p, _, _ := strings.Cut(k, "?")
		if m := mediaRefRe.FindStringSubmatch(k); m != nil {
			if id, err := strconv.ParseInt(m[2], 10, 64); err != nil || withheld(id) {
				delete(st.items, k)
				removed++
			}
			continue
		}
		if !strings.HasPrefix(p, "/api/") || !isJSON(it.MIME) || !strings.Contains(string(it.Body), `"clipName"`) {
			continue
		}
		var v any
		dec := json.NewDecoder(strings.NewReader(string(it.Body)))
		dec.UseNumber()
		if dec.Decode(&v) != nil {
			continue
		}
		if n := blankClips(v, withheld); n > 0 {
			if b, err := json.Marshal(v); err == nil {
				it.Body = b
				blanked += n
			}
		}
	}
	if removed+blanked > 0 {
		log.Printf("clips: withheld %d recorded clip/spectrogram file(s), hid %d clip reference(s) from skipped or unknown sources", removed, blanked)
	}
}

// blankClips empties clipName on every detection record in v that withheld
// matches, and returns how many it changed.
func blankClips(v any, withheld func(int64) bool) int {
	n := 0
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			n += blankClips(e, withheld)
		}
	case map[string]any:
		if num, ok := t["id"].(json.Number); ok {
			if clip, ok := t["clipName"].(string); ok && clip != "" {
				if id, err := num.Int64(); err == nil && withheld(id) {
					t["clipName"] = ""
					n++
				}
			}
		}
		for _, e := range t {
			n += blankClips(e, withheld)
		}
	}
	return n
}

// fetchDetails fills in the detail-page API URLs (st.templates) for every
// detection, newest first, so clicking any recorded detection works.
func fetchDetails(ctx context.Context, c config, st *store, dets map[int64]detection) error {
	if len(st.templates) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(dets))
	for id := range dets {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	if len(ids) > c.MaxClips {
		ids = ids[:c.MaxClips]
	}
	client := &http.Client{Timeout: 60 * time.Second}
	var (
		wg              sync.WaitGroup
		sem             = make(chan struct{}, 4)
		mu              sync.Mutex
		fetched, failed int
	)
	for _, id := range ids {
		for _, t := range st.templates {
			if strings.Contains(t, "{sci}") && dets[id].Sci == "" {
				continue
			}
			k := strings.ReplaceAll(t, "{id}", fmt.Sprint(id))
			k = strings.ReplaceAll(k, "{sci}", encodeURIComponent(dets[id].Sci))
			if st.has(k) {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				body, mime, err := get(ctx, client, c.BirdNET+k)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					failed++
					return
				}
				fetched++
				st.put(&capture{Key: k, MIME: mime, Body: body})
			}()
		}
	}
	wg.Wait()
	log.Printf("details: %d detections × %d templates, %d fetched, %d failed", len(ids), len(st.templates), fetched, failed)
	return ctx.Err()
}

func collectDetections(v any, out map[int64]detection) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			collectDetections(e, out)
		}
	case map[string]any:
		if id, ok := t["id"].(float64); ok {
			if clip, ok := t["clipName"].(string); ok {
				d := detection{ID: int64(id), ClipName: clip}
				d.Sci, _ = t["scientificName"].(string)
				if src, ok := t["source"].(map[string]any); ok {
					s1, _ := src["id"].(string)
					s2, _ := src["displayName"].(string)
					d.Source = strings.ToLower(s1 + " " + s2)
				} else if s, ok := t["source"].(string); ok {
					d.Source = strings.ToLower(s)
				}
				out[d.ID] = d
			}
		}
		for _, e := range t {
			collectDetections(e, out)
		}
	}
}

func skipSource(src string, skip []string) bool {
	if src == "" {
		return true // unknown source: don't risk publishing it
	}
	for _, s := range skip {
		if strings.Contains(src, s) {
			return true
		}
	}
	return false
}

func get(ctx context.Context, client *http.Client, u string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("%s: status %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	return b, resp.Header.Get("Content-Type"), err
}

func isJSON(mime string) bool { return strings.Contains(mime, "json") }

var (
	statusRe = regexp.MustCompile(`^/api/v2/spectrogram/(\d+)/status(\?.*)?$`)
	mediaRe  = regexp.MustCompile(`^/api/v2/(spectrogram/\d+(\?.*)?|audio/\d+)$`)
)

// retryMedia fetches spectrograms and clips the UI wanted but did not get
// during the crawl: images still rendering when a page asked (the UI polls a
// status endpoint, which the snapshot answers itself from what is recorded),
// and clips not yet written for brand-new detections. A GET renders a
// spectrogram on demand.
func retryMedia(ctx context.Context, c config, st *store) error {
	want := map[string]bool{}
	st.mu.Lock()
	for k := range st.items {
		if m := statusRe.FindStringSubmatch(k); m != nil {
			want["/api/v2/spectrogram/"+m[1]+m[2]] = true
		}
	}
	for _, miss := range st.misses {
		if _, k, ok := strings.Cut(miss, " "); ok && mediaRe.MatchString(k) {
			want[k] = true
		}
	}
	for k := range want {
		if _, ok := st.items[k]; ok {
			delete(want, k)
		}
	}
	st.mu.Unlock()
	if len(want) == 0 {
		return nil
	}
	client := &http.Client{Timeout: 60 * time.Second}
	var (
		wg      sync.WaitGroup
		sem     = make(chan struct{}, 4)
		mu      sync.Mutex
		fetched int
	)
	for k := range want {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			body, mime, err := get(ctx, client, c.BirdNET+k)
			if err != nil {
				return
			}
			mu.Lock()
			fetched++
			mu.Unlock()
			st.put(&capture{Key: k, MIME: mime, Body: body})
		}()
	}
	wg.Wait()
	log.Printf("media: %d of %d missing spectrograms/clips recovered", fetched, len(want))
	return ctx.Err()
}

// encodeURIComponent escapes s the way the browser does in the UI's URLs
// ("Corvus corax" -> "Corvus%20corax").
func encodeURIComponent(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
