package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The audit looks for the signatures of a noisy microphone: one species
// swamping the station, one microphone behind most of a species, daytime birds
// heard at night, and species made of low-confidence single-model hits. It
// reads BirdNET-Go and reports; it never changes anything.

type auditConfig struct {
	Hours          int     // look back this far
	MinTotal       int     // kept detections needed before shares mean anything
	DominantShare  float64 // a species this large is worth a look when its evidence is thin
	SourceShare    float64 // one source behind this share of a species
	SourceMin      int     // detections of a species before judging where they came from
	SourceHeard    float64 // and the species is this share of what that source heard
	SourceTopShare float64 // two species make up this share of one source
	SourceTopMin   int     // detections from a source before judging it
	NightHits      int     // night detections of a daytime species
	NightShare     float64 // and this share of its detections
	LowConfMin     int     // detections before judging a species' confidence
	LowConfMedian  float64 // median confidence under this ...
	LowConfMulti   float64 // ... with fewer than this share confirmed by two models
	DoubtMean      float64 // thin evidence: mean confidence under this
	DoubtMulti     float64 // or two-model share under this
	Nocturnal      *regexp.Regexp
	Ignore         *regexp.Regexp
}

func defaultAudit() auditConfig {
	return auditConfig{
		Hours: 48, MinTotal: 100, DominantShare: 0.25,
		SourceShare: 0.7, SourceMin: 30, SourceHeard: 0.3, SourceTopShare: 0.6, SourceTopMin: 100,
		NightHits: 3, NightShare: 0.5,
		LowConfMin: 50, LowConfMedian: 0.45, LowConfMulti: 0.4,
		DoubtMean: 0.55, DoubtMulti: 0.4,
		Nocturnal: regexp.MustCompile(`(?i)owl|nightjar|nighthawk|night[- ]heron|poorwill|whip-poor|\brail\b|coyote|\bdog\b|\bbat\b|frog|toad`),
	}
}

// finding is one thing the audit wants a person to look at.
type finding struct {
	Check    string `json:"check"` // species or source
	Key      string `json:"key"`
	Species  string `json:"species,omitempty"`
	Source   string `json:"source,omitempty"`
	Severity int    `json:"severity"` // 2 warning, 1 note
	Text     string `json:"text"`
}

// audit runs every check over the tallied detections.
func audit(st *detectionStats, cfg auditConfig) []finding {
	var out []finding
	if st.Kept == 0 {
		return out
	}
	ranked := st.ranked()
	doubtful := func(s *speciesStat) bool { return s.meanConf() < cfg.DoubtMean || s.multiShare() < cfg.DoubtMulti }
	ignored := func(s *speciesStat) bool {
		return cfg.Ignore != nil && (cfg.Ignore.MatchString(s.Common) || cfg.Ignore.MatchString(s.Sci))
	}
	manySources := len(st.BySource) >= 2

	// One finding per species, with every reason it tripped.
	for _, s := range ranked {
		if ignored(s) {
			continue
		}
		var reasons []string
		sev := 0
		if st.Kept >= cfg.MinTotal && share(s.N, st.Kept) >= cfg.DominantShare && doubtful(s) {
			reasons = append(reasons, fmt.Sprintf("%s of all detections (%s of %s)", pctStr(share(s.N, st.Kept)), commas(s.N), commas(st.Kept)))
			sev = 2
		}
		if manySources && s.N >= cfg.SourceMin {
			if src, n := s.topSource(); doubtful(s) && share(n, s.N) >= cfg.SourceShare && share(n, st.BySource[src]) >= cfg.SourceHeard {
				reasons = append(reasons, fmt.Sprintf("%s of them from %s, where it is %s of everything heard", pctStr(share(n, s.N)), src, pctStr(share(n, st.BySource[src]))))
				sev = 2
			}
		}
		if s.N >= cfg.LowConfMin && s.medianConf() < cfg.LowConfMedian && s.multiShare() < cfg.LowConfMulti {
			reasons = append(reasons, fmt.Sprintf("median confidence %.2f with %s confirmed by two models", s.medianConf(), pctStr(s.multiShare())))
			sev = 2
		}
		if s.Night >= cfg.NightHits && s.nightShare() >= cfg.NightShare && !cfg.Nocturnal.MatchString(s.Common+" "+s.Sci) {
			reasons = append(reasons, fmt.Sprintf("%d of %d detections at night (7 pm–6 am), unusual for a daytime bird", s.Night, s.N))
			if sev == 0 {
				sev = 1
			}
		}
		if len(reasons) > 0 {
			out = append(out, finding{Check: "species", Key: "species|" + s.Sci, Species: s.Common, Severity: sev,
				Text: s.Common + ": " + strings.Join(reasons, "; ") + "."})
		}
	}

	// A microphone that mostly reports two species.
	if manySources {
		srcs := make([]string, 0, len(st.BySource))
		for src := range st.BySource {
			srcs = append(srcs, src)
		}
		sort.Strings(srcs)
		for _, src := range srcs {
			total := st.BySource[src]
			if total < cfg.SourceTopMin {
				continue
			}
			top := topTwo(st.SourceSpecies[src])
			if len(top) < 2 {
				continue
			}
			a, b := st.Species[top[0]], st.Species[top[1]]
			if ignored(a) || ignored(b) {
				continue
			}
			ca, cb := st.SourceSpecies[src][a.Sci], st.SourceSpecies[src][b.Sci]
			if sh := share(ca+cb, total); sh >= cfg.SourceTopShare && (doubtful(a) || doubtful(b)) {
				var thin []string
				for _, s := range []*speciesStat{a, b} {
					if doubtful(s) {
						thin = append(thin, fmt.Sprintf("%s (median confidence %.2f, %s confirmed by two models)", s.Common, s.medianConf(), pctStr(s.multiShare())))
					}
				}
				out = append(out, finding{Check: "source", Key: "source|" + src, Source: src, Severity: 2,
					Text: fmt.Sprintf("%s: %s of its %s detections are %s (%s) and %s (%s); thin evidence for %s.",
						src, pctStr(sh), commas(total), a.Common, pctStr(share(ca, total)), b.Common, pctStr(share(cb, total)), joinAnd(thin))})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Severity > out[j].Severity })
	return out
}

// topTwo returns the two species with the most detections.
func topTwo(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 2 {
		keys = keys[:2]
	}
	return keys
}

// auditState remembers what was already reported, so a problem that stays
// unfixed is re-sent weekly rather than every night.
type auditState struct {
	Notified map[string]time.Time `json:"notified"`
}

func loadAuditState(path string) auditState {
	st := auditState{Notified: map[string]time.Time{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Notified == nil {
		st.Notified = map[string]time.Time{}
	}
	return st
}

func saveJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// stateFile is where a command keeps its memory: $XDG_STATE_HOME or ~/.local/state.
func stateFile(name string) string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "birdnet-snapshot", name)
}

// due returns the findings not reported within renotify, and the state with
// resolved findings forgotten (so one that comes back is reported again).
func due(fs []finding, st auditState, now time.Time, renotify time.Duration) ([]finding, auditState) {
	next := auditState{Notified: map[string]time.Time{}}
	var out []finding
	for _, f := range fs {
		if last, ok := st.Notified[f.Key]; ok && now.Sub(last) < renotify {
			next.Notified[f.Key] = last
			continue
		}
		out = append(out, f)
		next.Notified[f.Key] = now
	}
	return out, next
}

// auditRaw is set by -raw.
var auditRaw bool

func auditMain(args []string) error {
	cfg := defaultAudit()
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	base := fs.String("birdnet", env("BIRDNET_URL", "http://127.0.0.1:8080"), "BirdNET-Go base `url` [BIRDNET_URL]")
	fs.IntVar(&cfg.Hours, "hours", envInt("AUDIT_HOURS", cfg.Hours), "look back this many hours [AUDIT_HOURS]")
	fs.IntVar(&cfg.MinTotal, "min-total", envInt("AUDIT_MIN_TOTAL", cfg.MinTotal), "kept detections needed before shares are judged [AUDIT_MIN_TOTAL]")
	fs.Float64Var(&cfg.DominantShare, "dominant", envFloat("AUDIT_DOMINANT", cfg.DominantShare), "flag a species above this share of detections when its evidence is thin [AUDIT_DOMINANT]")
	fs.Float64Var(&cfg.SourceShare, "source-share", envFloat("AUDIT_SOURCE_SHARE", cfg.SourceShare), "flag a species this much of which came from one source [AUDIT_SOURCE_SHARE]")
	fs.IntVar(&cfg.NightHits, "night-hits", envInt("AUDIT_NIGHT_HITS", cfg.NightHits), "flag a daytime species with this many night detections [AUDIT_NIGHT_HITS]")
	nocturnal := fs.String("nocturnal", env("AUDIT_NOCTURNAL", ""), "extra `regex` of species that may call at night [AUDIT_NOCTURNAL]")
	ignore := fs.String("ignore", env("AUDIT_IGNORE", ""), "`regex` of species (common or scientific name) to leave out of every check [AUDIT_IGNORE]")
	renotify := fs.Duration("renotify", envDur("AUDIT_RENOTIFY", 7*24*time.Hour), "repeat an unresolved finding after this long [AUDIT_RENOTIFY]")
	state := fs.String("state", env("AUDIT_STATE", stateFile("audit.json")), "`file` remembering what was reported [AUDIT_STATE]")
	metrics := fs.String("metrics-file", env("AUDIT_METRICS_FILE", ""), "write Prometheus textfile metrics to `path` [AUDIT_METRICS_FILE]")
	notify := ntfyFromEnv()
	fs.StringVar(&notify.URL, "ntfy", notify.URL, "ntfy topic `url` to push findings to [NTFY_URL]")
	fs.StringVar(&notify.Token, "ntfy-token", notify.Token, "ntfy access `token` [NTFY_TOKEN]")
	fs.StringVar(&notify.Link, "link", notify.Link, "BirdNET-Go address as a phone sees it, for the tap link [BIRDNET_LINK]")
	dry := fs.Bool("dry", false, "print findings; send and remember nothing")
	raw := fs.Bool("raw", false, "count detections marked false positive too: what the audit would have said before review")
	asJSON := fs.Bool("json", false, "print findings as JSON")
	_ = fs.Parse(args)
	if *nocturnal != "" {
		re, err := regexp.Compile(`(?i)(` + cfg.Nocturnal.String() + `)|(` + *nocturnal + `)`)
		if err != nil {
			return fmt.Errorf("-nocturnal: %w", err)
		}
		cfg.Nocturnal = re
	}
	if *ignore != "" {
		re, err := regexp.Compile(*ignore)
		if err != nil {
			return fmt.Errorf("-ignore: %w", err)
		}
		cfg.Ignore = re
	}
	*base = trimSlash(*base)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	auditRaw = *raw
	err := runAudit(ctx, *base, cfg, notify, *state, *renotify, *metrics, *dry, *asJSON, start)
	if err != nil && !errors.Is(err, context.Canceled) {
		if *metrics != "" {
			_ = writeAuditMetrics(*metrics, nil, 0, start, err)
		}
		return err
	}
	return nil
}

func runAudit(ctx context.Context, base string, cfg auditConfig, notify ntfyFlags, statePath string, renotify time.Duration, metrics string, dry, asJSON bool, start time.Time) error {
	client := &http.Client{Timeout: 60 * time.Second}
	loc, _ := serverZone(ctx, base)
	now := time.Now().In(loc)
	from, to := dateRange(now, cfg.Hours)
	all, err := fetchDetections(ctx, client, base, from, to)
	if err != nil {
		return fmt.Errorf("detections: %w", err)
	}
	cutoff := now.Add(-time.Duration(cfg.Hours) * time.Hour)
	var dets []apiDetection
	for _, d := range all {
		if !d.when().Before(cutoff) {
			if auditRaw {
				d.Verified = ""
			}
			dets = append(dets, d)
		}
	}
	st := summarise(dets)
	found := audit(st, cfg)

	if asJSON {
		b, _ := json.MarshalIndent(found, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("audit of the last %d h: %s kept, %s removed, %d species\n", cfg.Hours, commas(st.Kept), commas(st.Removed), len(st.Species))
		for _, f := range found {
			fmt.Printf("  %s %s\n", map[int]string{2: "!", 1: "-"}[f.Severity], f.Text)
		}
		if len(found) == 0 {
			fmt.Println("  nothing to look at")
		}
	}
	if metrics != "" {
		if err := writeAuditMetrics(metrics, found, st.Kept, start, nil); err != nil {
			log.Printf("audit: metrics: %v", err)
		}
	}
	if dry {
		return nil
	}
	old := loadAuditState(statePath)
	fresh, next := due(found, old, now, renotify)
	if len(fresh) > 0 && notify.enabled() {
		if err := notify.send(ctx, auditMessage(fresh, len(found), cfg.Hours, notify.Link)); err != nil {
			return err // state stays as it was, so tomorrow retries
		}
		log.Printf("audit: sent %d finding(s) to ntfy", len(fresh))
	} else if len(fresh) > 0 {
		log.Printf("audit: %d finding(s), no ntfy topic configured", len(fresh))
	}
	return saveJSONFile(statePath, next)
}

// auditMessage is the push notification for findings not yet reported.
func auditMessage(fresh []finding, total, hours int, link string) ntfyMessage {
	var b strings.Builder
	prio := 3
	for _, f := range fresh {
		b.WriteString("• " + f.Text + "\n")
		if f.Severity == 2 && len(fresh) >= 3 {
			prio = 4
		}
	}
	b.WriteString("\nReview in BirdNET-Go: mark false positives, or give a species a fixed threshold.")
	title := "BirdNET audit: " + plural(len(fresh), "thing", "things") + " to look at"
	if total > len(fresh) {
		title += fmt.Sprintf(" (%d still open)", total-len(fresh))
	}
	m := ntfyMessage{Title: title, Message: b.String(), Priority: prio, Tags: []string{"mag"}}
	if link != "" {
		m.Click = link + "/ui/dashboard"
	}
	return m
}

func writeAuditMetrics(path string, found []finding, kept int, start time.Time, runErr error) error {
	lastSuccess := readGauge(path, "birdnet_audit_last_success_timestamp_seconds")
	ok := 0
	if runErr == nil {
		ok = 1
		lastSuccess = float64(time.Now().Unix())
	}
	var b strings.Builder
	g := func(name, help string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, strconv.FormatFloat(v, 'f', -1, 64))
	}
	g("birdnet_audit_last_run_timestamp_seconds", "Unix time the last audit started.", float64(start.Unix()))
	g("birdnet_audit_last_success_timestamp_seconds", "Unix time of the last audit that ran.", lastSuccess)
	g("birdnet_audit_last_run_success", "1 if the last audit ran.", float64(ok))
	g("birdnet_audit_kept_detections", "Detections the last audit looked at.", float64(kept))
	counts := map[string]int{"species": 0, "source": 0}
	warn := 0
	for _, f := range found {
		counts[f.Check]++
		if f.Severity == 2 {
			warn++
		}
	}
	g("birdnet_audit_warnings", "Warning findings in the last audit.", float64(warn))
	b.WriteString("# HELP birdnet_audit_findings Findings in the last audit, by check.\n# TYPE birdnet_audit_findings gauge\n")
	for _, check := range []string{"species", "source"} {
		fmt.Fprintf(&b, "birdnet_audit_findings{check=%q} %d\n", check, counts[check])
	}
	return writeTextfile(path, b.String())
}
