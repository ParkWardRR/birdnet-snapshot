package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

// A rare visitor is a species the range model calls uncommon here (or one the
// station has never heard before), confirmed by more than a lone hit: two or
// more detections close together, on two microphones, from two models, or at
// 0.9+. Lone hits are exactly what noise looks like (a Goldeneye, a Pygmy-Owl),
// so they wait for a second one.

type rareConfig struct {
	Window    time.Duration // detections this close together count as one visit
	MinHits   int
	MinConf   float64
	RarityMax float64       // range-model score under this means uncommon or rarer here
	NewMax    float64       // a first-ever species is worth a note under this score
	Gap       time.Duration // one alert per species per this long
}

func defaultRare() rareConfig {
	return rareConfig{Window: 90 * time.Minute, MinHits: 2, MinConf: 0.6, RarityMax: 0.5, NewMax: 0.8, Gap: 24 * time.Hour}
}

type rareVisitor struct {
	Sci, Common string
	Hits        int
	Sources     int
	Best        apiDetection
	Multi       bool
	Status      string
	Score       float64
	New         bool
	First, Last time.Time
}

// rarity is what the range model says about a species here.
type rarity struct {
	Status string
	Score  float64
}

// findVisitors picks the species in dets (detections inside the window ending
// at now) that deserve an alert.
func findVisitors(dets []apiDetection, now time.Time, cfg rareConfig, lookup func(sci string) (rarity, bool), firstHeard map[string]time.Time) []rareVisitor {
	groups := map[string][]apiDetection{}
	for _, d := range dets {
		t := d.when()
		if d.isRemoved() || d.Confidence < cfg.MinConf || t.After(now) || now.Sub(t) > cfg.Window {
			continue
		}
		groups[d.Sci] = append(groups[d.Sci], d)
	}
	var out []rareVisitor
	for sci, ds := range groups {
		if len(ds) < cfg.MinHits {
			continue
		}
		v := rareVisitor{Sci: sci, Common: ds[0].Common, Hits: len(ds), Best: ds[0]}
		sources := map[string]bool{}
		for i, d := range ds {
			sources[d.source()] = true
			v.Multi = v.Multi || d.multi()
			if d.Confidence > v.Best.Confidence || (d.Confidence == v.Best.Confidence && d.multi() && !v.Best.multi()) {
				v.Best = d
			}
			if t := d.when(); i == 0 || t.Before(v.First) {
				v.First = t
			}
			if t := d.when(); t.After(v.Last) {
				v.Last = t
			}
		}
		v.Sources = len(sources)
		if v.Sources < 2 && !v.Multi && v.Best.Confidence < 0.9 {
			continue // one microphone, one model, middling confidence
		}
		r, ok := lookup(sci)
		if !ok {
			continue
		}
		v.Status, v.Score = r.Status, r.Score
		if first, ok := firstHeard[sci]; ok && !first.Before(now.Add(-cfg.Window-time.Hour)) {
			v.New = true
		}
		if r.Score < cfg.RarityMax || (v.New && r.Score < cfg.NewMax) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score < out[j].Score })
	return out
}

func spanText(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	switch {
	case m < 1:
		return "under a minute"
	case m < 60:
		return plural(m, "minute", "minutes")
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

func rareMessage(v rareVisitor, link string) ntfyMessage {
	var b strings.Builder
	fmt.Fprintf(&b, "%s in %s on %s. Best %s", plural(v.Hits, "detection", "detections"), spanText(v.Last.Sub(v.First)),
		plural(v.Sources, "microphone", "microphones"), pctStr(v.Best.Confidence))
	if v.Multi {
		b.WriteString(", confirmed by two models")
	}
	fmt.Fprintf(&b, ".\n%s here (range score %.2f).", strings.ToUpper(v.Status[:1])+strings.ReplaceAll(v.Status[1:], "_", " "), v.Score)
	if v.New {
		b.WriteString(" First time at the station.")
	}
	m := ntfyMessage{Title: "Rare visitor: " + v.Common, Message: b.String(), Priority: 4, Tags: []string{"bird"}}
	if link != "" {
		m.Click = fmt.Sprintf("%s/ui/detections/%d", link, v.Best.ID)
	}
	return m
}

type rareState struct {
	Alerted map[string]time.Time `json:"alerted"`
}

func rareMain(args []string) error {
	cfg := defaultRare()
	fs := flag.NewFlagSet("rare", flag.ExitOnError)
	base := fs.String("birdnet", env("BIRDNET_URL", "http://127.0.0.1:8080"), "BirdNET-Go base `url` [BIRDNET_URL]")
	fs.DurationVar(&cfg.Window, "window", envDur("RARE_WINDOW", cfg.Window), "detections this close together count as one visit [RARE_WINDOW]")
	fs.IntVar(&cfg.MinHits, "min-hits", envInt("RARE_MIN_HITS", cfg.MinHits), "detections needed before alerting [RARE_MIN_HITS]")
	fs.Float64Var(&cfg.MinConf, "min-conf", envFloat("RARE_MIN_CONF", cfg.MinConf), "lowest confidence that counts [RARE_MIN_CONF]")
	fs.Float64Var(&cfg.RarityMax, "rarity", envFloat("RARE_RARITY", cfg.RarityMax), "range-model score under which a species counts as rare here [RARE_RARITY]")
	fs.Float64Var(&cfg.NewMax, "new", envFloat("RARE_NEW", cfg.NewMax), "a first-ever species alerts under this range-model score [RARE_NEW]")
	fs.DurationVar(&cfg.Gap, "gap", envDur("RARE_GAP", cfg.Gap), "alert a species at most once per this long [RARE_GAP]")
	state := fs.String("state", env("RARE_STATE", stateFile("rare.json")), "`file` remembering what was alerted [RARE_STATE]")
	notify := ntfyFromEnv()
	fs.StringVar(&notify.URL, "ntfy", notify.URL, "ntfy topic `url` [NTFY_URL]")
	fs.StringVar(&notify.Token, "ntfy-token", notify.Token, "ntfy access `token` [NTFY_TOKEN]")
	fs.StringVar(&notify.Link, "link", notify.Link, "BirdNET-Go address as a phone sees it, for the tap link [BIRDNET_LINK]")
	dry := fs.Bool("dry", false, "print visitors; send and remember nothing")
	_ = fs.Parse(args)
	*base = trimSlash(*base)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := runRare(ctx, *base, cfg, notify, *state, *dry)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runRare(ctx context.Context, base string, cfg rareConfig, notify ntfyFlags, statePath string, dry bool) error {
	client := &http.Client{Timeout: 60 * time.Second}
	loc, _ := serverZone(ctx, base)
	now := time.Now().In(loc)
	from, to := dateRange(now, int(cfg.Window/time.Hour)+2)
	dets, err := fetchDetections(ctx, client, base, from, to)
	if err != nil {
		return fmt.Errorf("detections: %w", err)
	}
	// Cheap exit: nothing recent can be a visit.
	recent := false
	for _, d := range dets {
		if !d.isRemoved() && d.Confidence >= cfg.MinConf && now.Sub(d.when()) <= cfg.Window {
			recent = true
			break
		}
	}
	if !recent {
		return nil
	}
	first := lifetimeFirstHeard(ctx, client, base, now)
	cache := map[string]*rarity{}
	lookup := func(sci string) (rarity, bool) {
		if r, ok := cache[sci]; ok {
			return *r, r != nil
		}
		b, _, err := get(ctx, client, base+"/api/v2/species?scientific_name="+url.QueryEscape(sci))
		var info struct {
			Rarity struct {
				Status string  `json:"status"`
				Score  float64 `json:"score"`
			} `json:"rarity"`
		}
		if err != nil || json.Unmarshal(b, &info) != nil || info.Rarity.Status == "" {
			cache[sci] = nil
			return rarity{}, false
		}
		r := &rarity{Status: info.Rarity.Status, Score: info.Rarity.Score}
		cache[sci] = r
		return *r, true
	}
	visitors := findVisitors(dets, now, cfg, lookup, first)

	st := rareState{Alerted: map[string]time.Time{}}
	if b, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(b, &st)
		if st.Alerted == nil {
			st.Alerted = map[string]time.Time{}
		}
	}
	for sci, t := range st.Alerted { // forget old alerts
		if now.Sub(t) > cfg.Gap {
			delete(st.Alerted, sci)
		}
	}
	sent := 0
	for _, v := range visitors {
		if _, done := st.Alerted[v.Sci]; done {
			continue
		}
		m := rareMessage(v, notify.Link)
		fmt.Printf("%s: %s\n", m.Title, strings.ReplaceAll(m.Message, "\n", " "))
		if dry {
			continue
		}
		if notify.enabled() {
			if err := notify.send(ctx, m); err != nil {
				return err
			}
			sent++
		}
		st.Alerted[v.Sci] = now
	}
	if dry {
		return nil
	}
	if sent > 0 {
		log.Printf("rare: sent %d alert(s)", sent)
	}
	return saveJSONFile(statePath, st)
}

// lifetimeFirstHeard maps each species to when the station first heard it
// (false positives excluded by BirdNET-Go). A failure just means no species is
// treated as new.
func lifetimeFirstHeard(ctx context.Context, client *http.Client, base string, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	b, _, err := get(ctx, client, base+"/api/v2/analytics/species/summary?start_date=2000-01-01&end_date="+now.Format("2006-01-02"))
	if err != nil {
		return out
	}
	var rows []struct {
		Sci   string `json:"scientific_name"`
		First string `json:"first_heard"`
	}
	if json.Unmarshal(b, &rows) != nil {
		return out
	}
	for _, r := range rows {
		if t, err := time.Parse(time.RFC3339, r.First); err == nil {
			out[r.Sci] = t
		}
	}
	return out
}
