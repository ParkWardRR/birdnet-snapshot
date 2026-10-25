// birdnet-snapshot publishes a read-only copy of a BirdNET-Go web UI.
//
// It drives the real UI in headless Chrome against a BirdNET-Go server,
// records every asset and API response the guest view loads, and writes a
// static site in which a service worker answers the UI's API calls from the
// recorded responses. The site can be synced to a Bunny storage zone or
// served by any static host, so visitors never reach the BirdNET-Go server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

type config struct {
	BirdNET     string        // BirdNET-Go base URL, e.g. http://127.0.0.1:8080
	Chrome      string        // DevTools URL of a running Chrome; empty launches one
	ChromePath  string        // Chrome binary to launch (empty: search the usual places)
	NoSandbox   bool          // launch Chrome with --no-sandbox (containers running as root)
	Out         string        // site output directory
	Days        int           // dashboard days to record, today included
	MaxPages    int           // page visit budget
	MaxClips    int           // detail page and audio clip budget
	SkipAudio   []string      // source names whose clips are never published
	NoAudio     bool          // publish no audio clips at all
	Routes      []string      // extra UI routes to record
	NoMobile    bool          // skip the phone-layout pass
	Label       string        // text of the snapshot pill
	Location    string        // coordinates in recorded JSON: hide, coarse or keep
	Redact      []string      // regexes replaced with "redacted" in recorded JSON
	Forbid      []string      // regexes that fail the build if found in any published file
	NoCSP       bool          // leave out the Content-Security-Policy meta tag
	AllowIndex  bool          // let search engines index the snapshot
	MinEntries  int           // refuse to build from a crawl with fewer API entries
	MaxShrink   float64       // refuse to deploy if entries fall below this share of the live snapshot
	Force       bool          // deploy despite the shrink guard
	Zone        string        // Bunny storage zone name
	ZoneHost    string        // Bunny storage endpoint, e.g. la.storage.bunnycdn.com
	ZoneKey     string        // Bunny storage zone password
	DryRun      bool          // deploy: show what would change without writing
	Metrics     string        // Prometheus textfile to write after each run
	Every       time.Duration // run: repeat at this interval instead of exiting
	PageWait    time.Duration // max wait for a page to go network-idle
	Budget      time.Duration // overall crawl time budget
	Concurrency int           // parallel uploads
}

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	switch cmd {
	case "version", "-version", "--version":
		fmt.Println("birdnet-snapshot", version)
		return
	case "help", "-h", "-help", "--help":
		usage()
	case "bunny-setup":
		if err := bunnySetupMain(os.Args[2:]); err != nil {
			log.Fatalf("bunny-setup: %v", err)
		}
		return
	}

	c, addr, rc := parseFlags(cmd, os.Args[2:])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "crawl":
		_, err = crawlAndBuild(ctx, c)
	case "deploy":
		_, err = deploy(ctx, c)
	case "run":
		err = runLoop(ctx, c)
	case "serve":
		err = serve(ctx, c, addr)
	case "check":
		err = check(ctx, c)
	case "report":
		if err = buildReport(ctx, c, rc); err == nil && rc.Deploy {
			_, err = deploy(ctx, c)
		}
	default:
		usage()
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("%s: %v", cmd, err)
	}
}

// parseFlags reads the shared flags; every flag falls back to an env var.
func parseFlags(cmd string, args []string) (config, string, reportConfig) {
	var c config
	var rc reportConfig
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	fs.StringVar(&c.BirdNET, "birdnet", env("BIRDNET_URL", "http://127.0.0.1:8080"), "BirdNET-Go base `url` [BIRDNET_URL]")
	fs.StringVar(&c.Chrome, "chrome", env("CHROME_URL", ""), "DevTools `url` of a running Chrome; empty launches one [CHROME_URL]")
	fs.StringVar(&c.ChromePath, "chrome-path", env("CHROME_PATH", ""), "Chrome `binary` to launch; empty searches the usual places [CHROME_PATH]")
	fs.BoolVar(&c.NoSandbox, "no-sandbox", envBool("CHROME_NO_SANDBOX", false), "launch Chrome with --no-sandbox, for containers running as root [CHROME_NO_SANDBOX]")
	fs.StringVar(&c.Out, "out", env("SNAPSHOT_OUT", "site"), "site output `dir` [SNAPSHOT_OUT]")
	fs.IntVar(&c.Days, "days", envInt("SNAPSHOT_DAYS", 7), "dashboard days to record, today included [SNAPSHOT_DAYS]")
	fs.IntVar(&c.MaxPages, "max-pages", envInt("SNAPSHOT_MAX_PAGES", 150), "page visit budget [SNAPSHOT_MAX_PAGES]")
	fs.IntVar(&c.MaxClips, "max-clips", envInt("SNAPSHOT_MAX_CLIPS", 1500), "detail page and audio clip budget, newest first [SNAPSHOT_MAX_CLIPS]")
	skip := fs.String("skip-audio", env("SKIP_AUDIO_SOURCES", ""), "comma-separated audio source `names` whose clips are never published, e.g. doorbell [SKIP_AUDIO_SOURCES]")
	fs.BoolVar(&c.NoAudio, "no-audio", envBool("SNAPSHOT_NO_AUDIO", false), "publish no audio clips at all [SNAPSHOT_NO_AUDIO]")
	routes := fs.String("routes", env("SNAPSHOT_ROUTES", ""), "extra comma-separated UI `routes` to record, e.g. /ui/analytics/advanced [SNAPSHOT_ROUTES]")
	fs.BoolVar(&c.NoMobile, "no-mobile", envBool("SNAPSHOT_NO_MOBILE", false), "skip the phone-layout pass [SNAPSHOT_NO_MOBILE]")
	fs.StringVar(&c.Label, "label", env("SNAPSHOT_LABEL", "Snapshot"), "`text` of the snapshot pill, followed by the time [SNAPSHOT_LABEL]")
	fs.StringVar(&c.Location, "location", env("SNAPSHOT_LOCATION", "hide"), "coordinates in recorded data: hide (remove), coarse (whole degrees, ~100 km) or keep [SNAPSHOT_LOCATION]")
	redact := fs.String("redact", env("SNAPSHOT_REDACT", ""), "comma-separated `regexes` replaced with \"redacted\" in recorded data [SNAPSHOT_REDACT]")
	forbid := fs.String("forbid", env("SNAPSHOT_FORBID", ""), "comma-separated `regexes` that fail the build if found in any published file, e.g. a home hostname [SNAPSHOT_FORBID]")
	fs.BoolVar(&c.NoCSP, "no-csp", envBool("SNAPSHOT_NO_CSP", false), "leave out the Content-Security-Policy meta tag [SNAPSHOT_NO_CSP]")
	fs.BoolVar(&c.AllowIndex, "allow-indexing", envBool("SNAPSHOT_ALLOW_INDEXING", false), "let search engines index the snapshot (default: noindex + robots.txt) [SNAPSHOT_ALLOW_INDEXING]")
	fs.IntVar(&c.MinEntries, "min-entries", envInt("SNAPSHOT_MIN_ENTRIES", 20), "refuse to build from a crawl with fewer API responses [SNAPSHOT_MIN_ENTRIES]")
	fs.Float64Var(&c.MaxShrink, "max-shrink", envFloat("SNAPSHOT_MAX_SHRINK", 0.5), "refuse to deploy if entries fall below this `share` of the live snapshot; 0 disables [SNAPSHOT_MAX_SHRINK]")
	fs.BoolVar(&c.Force, "force", false, "deploy despite the shrink guard")
	fs.StringVar(&c.Zone, "zone", env("BUNNY_STORAGE_ZONE", ""), "Bunny storage zone `name` [BUNNY_STORAGE_ZONE]")
	fs.StringVar(&c.ZoneHost, "zone-host", env("BUNNY_STORAGE_HOST", "storage.bunnycdn.com"), "Bunny storage `endpoint` [BUNNY_STORAGE_HOST]")
	fs.BoolVar(&c.DryRun, "dry", false, "deploy: show what would change without writing")
	fs.StringVar(&c.Metrics, "metrics-file", env("SNAPSHOT_METRICS_FILE", ""), "write Prometheus textfile metrics to `path` after each run [SNAPSHOT_METRICS_FILE]")
	fs.DurationVar(&c.Every, "every", envDur("SNAPSHOT_EVERY", 0), "run: repeat at this `interval` (e.g. 1h) instead of exiting [SNAPSHOT_EVERY]")
	fs.DurationVar(&c.PageWait, "page-wait", envDur("SNAPSHOT_PAGE_WAIT", 20*time.Second), "max wait for a page to go network-idle [SNAPSHOT_PAGE_WAIT]")
	fs.DurationVar(&c.Budget, "budget", envDur("SNAPSHOT_BUDGET", 20*time.Minute), "overall crawl time budget [SNAPSHOT_BUDGET]")
	fs.IntVar(&c.Concurrency, "concurrency", envInt("SNAPSHOT_CONCURRENCY", 8), "parallel uploads [SNAPSHOT_CONCURRENCY]")
	addr := fs.String("addr", env("SNAPSHOT_ADDR", "127.0.0.1:8765"), "serve: listen `address` [SNAPSHOT_ADDR]")
	fs.StringVar(&rc.Title, "title", env("REPORT_TITLE", "Field Notes"), "report: title [REPORT_TITLE]")
	fs.StringVar(&rc.Subtitle, "subtitle", env("REPORT_SUBTITLE", "A week of birdsong, as heard by the station"), "report: subtitle [REPORT_SUBTITLE]")
	fs.StringVar(&rc.Station, "station", env("REPORT_STATION", "A backyard listening station"), "report: station name shown on the cover; keep it vague [REPORT_STATION]")
	fs.BoolVar(&rc.PDF, "pdf", envBool("REPORT_PDF", true), "report: also print the report to a PDF [REPORT_PDF]")
	fs.BoolVar(&rc.Photos, "photos", envBool("REPORT_PHOTOS", true), "report: fetch large species photos from Avicommons (else BirdNET-Go's small ones) [REPORT_PHOTOS]")
	fs.IntVar(&rc.Clips, "clips", envInt("REPORT_CLIPS", 3), "report: publish this many best and this many most recent recordings, one per species (0 = none; -skip-audio and -no-audio apply) [REPORT_CLIPS]")
	fs.Float64Var(&rc.ClipMin, "clip-min", envFloat("REPORT_CLIP_MIN", 0.5), "report: lowest `confidence` a published recording may have [REPORT_CLIP_MIN]")
	fs.BoolVar(&rc.Deploy, "deploy", envBool("REPORT_DEPLOY", false), "report: deploy -out to the Bunny storage zone when built [REPORT_DEPLOY]")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: birdnet-snapshot %s [flags]\n\n", cmd)
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	c.ZoneKey = os.Getenv("BUNNY_STORAGE_KEY")
	c.SkipAudio = splitList(strings.ToLower(*skip))
	c.Routes = splitList(*routes)
	c.Redact = splitList(*redact)
	c.Forbid = splitList(*forbid)
	c.BirdNET = strings.TrimRight(c.BirdNET, "/")
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
	rc.Days = c.Days
	return c, *addr, rc
}

// runLoop runs crawl+deploy once, or every c.Every until the context ends.
func runLoop(ctx context.Context, c config) error {
	for {
		start := time.Now()
		err := runOnce(ctx, c)
		if c.Every <= 0 {
			return err
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			log.Printf("run failed: %v", err)
		}
		next := start.Add(c.Every)
		log.Printf("next run at %s", next.Format("15:04:05"))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(next)):
		}
	}
}

func runOnce(ctx context.Context, c config) error {
	r := &runReport{Start: time.Now()}
	b, err := crawlAndBuild(ctx, c)
	if b != nil {
		r.Build = *b
	}
	if err == nil {
		var d *deployResult
		d, err = deploy(ctx, c)
		if d != nil {
			r.Deploy = *d
		}
	}
	r.Err = err
	if c.Metrics != "" {
		if merr := writeMetrics(c.Metrics, r); merr != nil {
			log.Printf("metrics: %v", merr)
		}
	}
	return err
}

func crawlAndBuild(ctx context.Context, c config) (*buildResult, error) {
	start := time.Now()
	st, err := crawl(ctx, c)
	if err != nil {
		return nil, err
	}
	crawled := time.Since(start)
	if err := fetchClips(ctx, c, st); err != nil {
		return nil, err
	}
	if err := fetchAssetClosure(ctx, c, st); err != nil {
		return nil, err
	}
	if err := validate(c, st); err != nil {
		return nil, err
	}
	res, err := buildSite(c, st, start)
	if err != nil {
		return nil, err
	}
	res.CrawlDuration = crawled
	res.Duration = time.Since(start)
	log.Printf("built %s in %s", c.Out, res.Duration.Round(time.Second))
	return res, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(k), 64); err == nil {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func usage() {
	fmt.Fprintln(os.Stderr, `birdnet-snapshot `+version+` - publish a read-only snapshot of a BirdNET-Go web UI

usage: birdnet-snapshot <command> [flags]

commands:
  run          crawl, then deploy (add -every 1h to keep running)
  crawl        record the guest UI into -out
  deploy       sync -out to a Bunny storage zone (needs BUNNY_STORAGE_KEY)
  serve        preview -out locally the way the CDN serves it
  check        test the BirdNET-Go, Chrome and Bunny settings
  report       build Field Notes: an illustrated report page + PDF (-deploy to publish)
  bunny-setup  create the Bunny storage zone, pull zone, hostname and cert
  version      print the version

Run "birdnet-snapshot <command> -h" for the flags of a command.
Every flag can also be set with the environment variable shown in brackets.`)
	os.Exit(2)
}
