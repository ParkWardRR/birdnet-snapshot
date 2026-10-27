package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The report page: a SvelteKit app built into report/dist (see report/web)
// and embedded, so the binary needs no Node at run time.
//
//go:embed all:report/dist
var reportFS embed.FS

// reportData is data/report.json, everything the report page renders.
type reportData struct {
	Version     string            `json:"version"`
	GeneratedAt time.Time         `json:"generatedAt"`
	Title       string            `json:"title"`
	Subtitle    string            `json:"subtitle"`
	Station     string            `json:"station"`
	Period      reportPeriod      `json:"period"`
	PDF         string            `json:"pdf,omitempty"`
	Totals      reportTotals      `json:"totals"`
	Hourly      []int             `json:"hourly"`
	Daily       []reportDay       `json:"daily"`
	Species     []reportSpecies   `json:"species"`
	Highlights  []string          `json:"highlights"`
	Recordings  *reportRecordings `json:"recordings,omitempty"`
	Dawn        *reportDawn       `json:"dawn,omitempty"`
	Compare     *reportCompare    `json:"compare,omitempty"`
	Quality     *reportQuality    `json:"quality,omitempty"`
	Growth      *reportGrowth     `json:"growth,omitempty"`
	Weather     *reportWeather    `json:"weather,omitempty"`
	Soundscape  *reportSoundscape `json:"soundscape,omitempty"`
}

type reportPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Days  int    `json:"days"`
	Label string `json:"label"`
}

type reportTotals struct {
	Detections      int     `json:"detections"`
	Species         int     `json:"species"`
	NewSpecies      int     `json:"newSpecies"`
	ActiveDays      int     `json:"activeDays"`
	BusiestHour     int     `json:"busiestHour"`
	BusiestDay      string  `json:"busiestDay"`
	BusiestDayCount int     `json:"busiestDayCount"`
	NightShare      float64 `json:"nightShare"`
}

type reportDay struct {
	Date       string `json:"date"`
	Detections int    `json:"detections"`
	Species    int    `json:"species"`
}

type reportSpecies struct {
	Code          string       `json:"code"`
	Common        string       `json:"common"`
	Scientific    string       `json:"scientific"`
	Family        string       `json:"family,omitempty"`
	Order         string       `json:"order,omitempty"`
	Count         int          `json:"count"`
	Share         float64      `json:"share"`
	AvgConfidence float64      `json:"avgConfidence"`
	MaxConfidence float64      `json:"maxConfidence"`
	FirstHeard    string       `json:"firstHeard"`
	LastHeard     string       `json:"lastHeard"`
	DaysHeard     int          `json:"daysHeard"`
	Hourly        []int        `json:"hourly"`
	Daily         []int        `json:"daily"`
	PeakHour      int          `json:"peakHour"`
	NightShare    float64      `json:"nightShare"`
	Rarity        string       `json:"rarity,omitempty"`
	RarityScore   float64      `json:"rarityScore,omitempty"`
	IsNew         bool         `json:"isNew"`
	Photo         *reportPhoto `json:"photo,omitempty"`
	Prev          int          `json:"prev,omitempty"`   // detections in the period before, when there is one to compare
	Change        *float64     `json:"change,omitempty"` // per active day against that period (0.24 = up 24%)
	Clips         []reportClip `json:"clips,omitempty"`  // best recordings, for the species' own view
}

type reportPhoto struct {
	Large      string `json:"large"`
	Small      string `json:"small"`
	Author     string `json:"author,omitempty"`
	License    string `json:"license,omitempty"`
	LicenseURL string `json:"licenseUrl,omitempty"`
	Source     string `json:"source"`
}

// reportConfig holds the report command's own settings.
type reportConfig struct {
	Title        string
	Subtitle     string
	Station      string
	Days         int
	PDF          bool
	Deploy       bool
	Photos       bool    // fetch large photos from Avicommons
	Clips        int     // best and most recent recordings to publish (each)
	ClipMin      float64 // lowest confidence a published recording may have
	Dawn         bool    // chart the dawn-chorus onset of the morning singers
	Weather      bool    // chart activity against the hourly weather BirdNET-Go stores
	SpeciesClips int     // recordings per species for the most-heard species (0 = none)

	// Prometheus holds BirdNET-Go's sound-level metrics; without it there is no soundscape.
	Prometheus     string
	PrometheusUser string
	PrometheusPass string
	SoundMetric    string
}

// buildReport gathers the last rc.Days days from BirdNET-Go and writes the
// report site (page, data, photos, recordings, PDF) to c.Out.
func buildReport(ctx context.Context, c config, rc reportConfig) error {
	start := time.Now()
	loc, _ := serverZone(ctx, c.BirdNET)
	end := time.Now().In(loc)
	if rc.Days < 1 {
		rc.Days = 7
	}
	first := end.AddDate(0, 0, -(rc.Days - 1))
	d, dets, err := collectReport(ctx, c, rc, loc, first, end, end)
	if err != nil {
		return err
	}
	tmp := c.Out + ".tmp"
	if err := writeReportSite(ctx, c, rc, d, dets, tmp); err != nil {
		return err
	}
	// -forbid applies to the report data too.
	red, err := newRedactor(c)
	if err != nil {
		return err
	}
	if b, err := os.ReadFile(filepath.Join(tmp, "data", "report.json")); err != nil {
		return err
	} else if err := red.check("data/report.json", b); err != nil {
		return err
	}
	if err := os.RemoveAll(c.Out); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.Out); err != nil {
		return err
	}
	log.Printf("report: %s, %d species, %d detections, built %s in %s", d.Period.Label, d.Totals.Species, d.Totals.Detections, c.Out, time.Since(start).Round(time.Second))
	return nil
}

// daysBetween counts the calendar days from first to end, both included.
func daysBetween(first, end time.Time) int {
	a, _ := time.Parse("2006-01-02", first.Format("2006-01-02"))
	b, _ := time.Parse("2006-01-02", end.Format("2006-01-02"))
	return int(b.Sub(a).Hours()/24+0.5) + 1
}

// collectReport reads everything the report says about the days first..end
// from BirdNET-Go: counts, hourly profiles, rarity, how the period compares
// with the one before, quality, growth, weather and sound. It also returns the
// period's detections, which the recordings are chosen from. now is the
// current time in the station's zone.
func collectReport(ctx context.Context, c config, rc reportConfig, loc *time.Location, first, end, now time.Time) (*reportData, []apiDetection, error) {
	days := daysBetween(first, end)
	client := &http.Client{Timeout: 60 * time.Second}
	api := func(p string, v any) error {
		b, _, err := get(ctx, client, c.BirdNET+"/api/v2"+p)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}

	// Period summary: one row per species.
	var summary []struct {
		Sci       string  `json:"scientific_name"`
		Common    string  `json:"common_name"`
		Code      string  `json:"species_code"`
		Count     int     `json:"count"`
		First     string  `json:"first_heard"`
		Last      string  `json:"last_heard"`
		AvgConf   float64 `json:"avg_confidence"`
		MaxConf   float64 `json:"max_confidence"`
		Thumbnail string  `json:"thumbnail_url"`
	}
	if err := api(fmt.Sprintf("/analytics/species/summary?start_date=%s&end_date=%s", first.Format("2006-01-02"), end.Format("2006-01-02")), &summary); err != nil {
		return nil, nil, fmt.Errorf("species summary: %w", err)
	}
	if len(summary) == 0 {
		return nil, nil, fmt.Errorf("no detections between %s and %s; not building a report", first.Format("2006-01-02"), end.Format("2006-01-02"))
	}

	d := &reportData{
		Version: version, GeneratedAt: now.Truncate(time.Second),
		Title: rc.Title, Subtitle: rc.Subtitle, Station: rc.Station,
		Period: reportPeriod{Start: first.Format("2006-01-02"), End: end.Format("2006-01-02"), Days: days, Label: periodLabel(first, end)},
		Hourly: make([]int, 24),
	}
	idx := map[string]*reportSpecies{}
	for _, s := range summary {
		sp := &reportSpecies{
			Code: s.Code, Common: s.Common, Scientific: s.Sci, Count: s.Count,
			AvgConfidence: round2(s.AvgConf), MaxConfidence: round2(s.MaxConf),
			FirstHeard: s.First, LastHeard: s.Last, Hourly: make([]int, 24), Daily: make([]int, days),
		}
		d.Species = append(d.Species, *sp)
		d.Totals.Detections += s.Count
	}
	for i := range d.Species {
		idx[d.Species[i].Scientific] = &d.Species[i]
	}

	// Per-day, per-hour counts.
	hourlyByDay := make([][]int, days)
	for i := 0; i < days; i++ {
		day := first.AddDate(0, 0, i).Format("2006-01-02")
		hourlyByDay[i] = make([]int, 24)
		var rows []struct {
			Sci    string `json:"scientific_name"`
			Count  int    `json:"count"`
			Hourly []int  `json:"hourly_counts"`
			IsNew  bool   `json:"is_new_species"`
		}
		if err := api("/analytics/species/daily?date="+day+"&limit=1000", &rows); err != nil {
			return nil, nil, fmt.Errorf("daily %s: %w", day, err)
		}
		dd := reportDay{Date: day}
		for _, r := range rows {
			sp := idx[r.Sci]
			if sp == nil || r.Count == 0 {
				continue
			}
			dd.Detections += r.Count
			dd.Species++
			sp.Daily[i] = r.Count
			sp.DaysHeard++
			if r.IsNew {
				sp.IsNew = true
			}
			for h := 0; h < 24 && h < len(r.Hourly); h++ {
				sp.Hourly[h] += r.Hourly[h]
				d.Hourly[h] += r.Hourly[h]
				hourlyByDay[i][h] += r.Hourly[h]
			}
		}
		d.Daily = append(d.Daily, dd)
		if dd.Detections > 0 {
			d.Totals.ActiveDays++
		}
		if dd.Detections > d.Totals.BusiestDayCount {
			d.Totals.BusiestDay, d.Totals.BusiestDayCount = day, dd.Detections
		}
	}

	// Taxonomy and rarity (coordinates in that response are never copied).
	for i := range d.Species {
		sp := &d.Species[i]
		var info struct {
			Rarity struct {
				Status string  `json:"status"`
				Score  float64 `json:"score"`
			} `json:"rarity"`
			Taxonomy struct {
				Order        string `json:"order"`
				FamilyCommon string `json:"family_common"`
			} `json:"taxonomy"`
		}
		if err := api("/species?scientific_name="+encodeURIComponent(sp.Scientific), &info); err == nil {
			sp.Rarity = strings.ReplaceAll(info.Rarity.Status, "_", " ")
			sp.RarityScore = round2(info.Rarity.Score)
			sp.Family, sp.Order = info.Taxonomy.FamilyCommon, info.Taxonomy.Order
		}
	}

	skip := trimLeadingQuiet(d, first)
	hourlyByDay = hourlyByDay[skip:]
	finishTotals(d)
	sort.SliceStable(d.Species, func(i, j int) bool { return d.Species[i].Count > d.Species[j].Count })
	if err := collectCompare(ctx, api, d, first, days); err != nil {
		log.Printf("report: no comparison with the period before (%v)", err)
	}
	d.Highlights = highlights(d)
	if rc.Dawn {
		fetchDawn(ctx, c, d, now)
	}

	// The period's detections feed the quality panel, the recordings and the
	// species views.
	dets, err := fetchDetections(ctx, client, c.BirdNET, d.Period.Start, d.Period.End)
	if err != nil {
		return nil, nil, fmt.Errorf("detections: %w", err)
	}
	d.Quality = qualityOf(dets, d)
	if g, err := collectGrowth(ctx, api, end, loc); err != nil {
		log.Printf("report: no species growth (%v)", err)
	} else {
		d.Growth = g
	}
	if rc.Weather {
		d.Weather = collectWeather(ctx, api, d, hourlyByDay)
	}
	if rc.Prometheus != "" {
		if s, err := collectSoundscape(ctx, rc, d, loc, now); err != nil {
			log.Printf("report: no soundscape (%v)", err)
		} else {
			d.Soundscape = s
		}
	}
	return d, dets, nil
}

// writeReportSite writes the report as a site into dir: the embedded page,
// the photos and recordings it shows, data/report.json and the PDF.
func writeReportSite(ctx context.Context, c config, rc reportConfig, d *reportData, dets []apiDetection, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	dist, err := fs.Sub(reportFS, "report/dist")
	if err != nil {
		return err
	}
	if err := os.CopyFS(dir, dist); err != nil {
		return fmt.Errorf("copy page: %w", err)
	}
	save, err := snapSaver(dir)
	if err != nil {
		return err
	}
	if err := fetchPhotos(ctx, c, rc, d, save); err != nil {
		return err
	}
	if err := fetchRecordings(ctx, c, rc, d, dets, save); err != nil {
		return err
	}
	fetchSpeciesClips(ctx, c, rc, d, dets, save)
	if err := writeReportJSON(dir, d); err != nil {
		return err
	}
	if rc.PDF {
		pdfName, err := printReportPDF(ctx, c, dir)
		if err != nil {
			return fmt.Errorf("pdf: %w", err)
		}
		d.PDF = "/" + pdfName
		if err := writeReportJSON(dir, d); err != nil {
			return err
		}
	}
	return nil
}

func finishTotals(d *reportData) {
	d.Totals.Species = len(d.Species)
	night := 0
	for h, n := range d.Hourly {
		if isNight(h) {
			night += n
		}
		if n > d.Hourly[d.Totals.BusiestHour] {
			d.Totals.BusiestHour = h
		}
	}
	sum := 0
	for _, n := range d.Hourly {
		sum += n
	}
	if sum > 0 {
		d.Totals.NightShare = round2(float64(night) / float64(sum))
	}
	for i := range d.Species {
		sp := &d.Species[i]
		if sp.IsNew {
			d.Totals.NewSpecies++
		}
		if d.Totals.Detections > 0 {
			sp.Share = round2(float64(sp.Count) / float64(d.Totals.Detections))
		}
		total, n := 0, 0
		for h, c := range sp.Hourly {
			total += c
			if isNight(h) {
				n += c
			}
			if c > sp.Hourly[sp.PeakHour] {
				sp.PeakHour = h
			}
		}
		if total > 0 {
			sp.NightShare = round2(float64(n) / float64(total))
		}
	}
}

// isNight is a fixed 7 pm–6 am window: real sunrise times would reveal the
// station's latitude.
func isNight(h int) bool { return h >= 19 || h < 6 }

// highlights writes the report's short narrative from the numbers.
func highlights(d *reportData) []string {
	var out []string
	if len(d.Species) == 0 {
		return out
	}
	top := d.Species[0]
	out = append(out, fmt.Sprintf("The %s led the period with %s detections, %d%% of everything heard.",
		top.Common, commas(top.Count), int(math.Round(top.Share*100))))
	out = append(out, compareHeadline(d)...)

	var owls []reportSpecies
	for _, sp := range d.Species {
		if sp.Count >= 20 && sp.NightShare >= 0.7 {
			owls = append(owls, sp)
		}
	}
	if len(owls) > 0 {
		names := make([]string, 0, 3)
		for i, sp := range owls {
			if i == 3 {
				break
			}
			names = append(names, sp.Common)
		}
		out = append(out, fmt.Sprintf("The night shift: %s called mostly between 7 pm and 6 am.", joinNames(names)))
	}

	// Dawn chorus: the morning hour with the most species singing.
	best, bestN := -1, 0
	for h := 5; h <= 9; h++ {
		n := 0
		for _, sp := range d.Species {
			if sp.Hourly[h] > 0 {
				n++
			}
		}
		if n > bestN {
			best, bestN = h, n
		}
	}
	if best >= 0 && bestN > 1 {
		out = append(out, fmt.Sprintf("The dawn chorus peaked at %s, with %d species singing in that hour.", hourLabel(best), bestN))
	}

	if d.Totals.BusiestDay != "" {
		day, _ := time.Parse("2006-01-02", d.Totals.BusiestDay)
		species := 0
		for _, dd := range d.Daily {
			if dd.Date == d.Totals.BusiestDay {
				species = dd.Species
			}
		}
		out = append(out, fmt.Sprintf("The busiest day was %s: %s detections across %d species.", day.Format("Monday, January 2"), commas(d.Totals.BusiestDayCount), species))
	}

	var rare []reportSpecies
	for _, sp := range d.Species {
		if sp.Rarity != "" && sp.Rarity != "very common" && sp.Rarity != "common" {
			rare = append(rare, sp)
		}
	}
	sort.Slice(rare, func(i, j int) bool { return rare[i].RarityScore < rare[j].RarityScore })
	if len(rare) > 0 {
		out = append(out, fmt.Sprintf("The rarest visitor was the %s, %s for this area and season.", rare[0].Common, rare[0].Rarity))
	}

	switch {
	case d.Totals.NewSpecies == d.Totals.Species && d.Totals.Species > 0:
		out = append(out, fmt.Sprintf("Every one of the %d species is a first for the station, which is still new.", d.Totals.Species))
	case d.Totals.NewSpecies > 0:
		var names []string
		for _, sp := range d.Species {
			if sp.IsNew {
				names = append(names, sp.Common)
			}
		}
		if len(names) > 4 {
			names = append(names[:4], fmt.Sprintf("%d more", len(names)-4))
		}
		out = append(out, fmt.Sprintf("New to the station: %s.", joinNames(names)))
	}
	return append(out, compareNotes(d)...)
}

// fetchPhotos stores a large and a small photo per species under /_snap/,
// named by content hash. Large photos come from Avicommons (900 px) when its
// catalogue knows the species; otherwise BirdNET-Go's own 320 px image.
func fetchPhotos(ctx context.Context, c config, rc reportConfig, d *reportData, save func([]byte, string) (string, error)) error {
	client := &http.Client{Timeout: 60 * time.Second}
	type avi struct {
		Code, Key, By, License string
	}
	catalogue := map[string]avi{}
	if rc.Photos {
		if b, _, err := get(ctx, client, "https://avicommons.org/latest.json"); err == nil {
			var all []avi
			if json.Unmarshal(b, &all) == nil {
				for _, a := range all {
					catalogue[a.Code] = a
				}
			}
		} else {
			log.Printf("report: Avicommons catalogue unavailable (%v); using BirdNET-Go photos", err)
		}
	}
	for i := range d.Species {
		sp := &d.Species[i]
		if a, ok := catalogue[sp.Code]; ok && a.Key != "" {
			large, _, err1 := get(ctx, client, fmt.Sprintf("https://static.avicommons.org/%s-%s-900.jpg", a.Code, a.Key))
			small, _, err2 := get(ctx, client, fmt.Sprintf("https://static.avicommons.org/%s-%s-480.jpg", a.Code, a.Key))
			if err1 == nil && err2 == nil {
				p := &reportPhoto{Author: a.By, Source: "Avicommons"}
				p.License, p.LicenseURL = licenseInfo(a.License)
				if p.Large, err1 = save(large, ".jpg"); err1 != nil {
					return err1
				}
				if p.Small, err1 = save(small, ".jpg"); err1 != nil {
					return err1
				}
				sp.Photo = p
				continue
			}
		}
		// Fallback: the photo BirdNET-Go shows, with its credit.
		img, _, err := get(ctx, client, c.BirdNET+"/api/v2/media/species-image?name="+encodeURIComponent(sp.Scientific))
		if err != nil {
			continue
		}
		var info struct {
			Author     string `json:"authorName"`
			License    string `json:"licenseName"`
			LicenseURL string `json:"licenseURL"`
			Source     string `json:"sourceProvider"`
		}
		if b, _, err := get(ctx, client, c.BirdNET+"/api/v2/media/species-image/info?name="+encodeURIComponent(sp.Scientific)); err == nil {
			_ = json.Unmarshal(b, &info)
		}
		name, err := save(img, ".jpg")
		if err != nil {
			return err
		}
		sp.Photo = &reportPhoto{Large: name, Small: name, Author: info.Author, License: info.License, LicenseURL: info.LicenseURL, Source: orDefault(info.Source, "BirdNET-Go")}
	}
	return nil
}

// snapSaver returns a function that stores a file under dir/_snap/, named by
// its content hash, and returns its site path.
func snapSaver(dir string) (func([]byte, string) (string, error), error) {
	if err := os.MkdirAll(filepath.Join(dir, "_snap"), 0o755); err != nil {
		return nil, err
	}
	return func(b []byte, ext string) (string, error) {
		sum := sha256.Sum256(b)
		name := "_snap/" + hex.EncodeToString(sum[:10]) + ext
		return "/" + name, os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), b, 0o644)
	}, nil
}

func writeReportJSON(dir string, d *reportData) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "data", "report.json"), b, 0o644)
}

// licenseInfo maps Avicommons license codes to a label and URL.
func licenseInfo(code string) (string, string) {
	c := strings.ToLower(strings.TrimSpace(code))
	switch {
	case strings.HasPrefix(c, "cc0"):
		return "CC0", "https://creativecommons.org/publicdomain/zero/1.0/"
	case c == "cc-by":
		return "CC BY 4.0", "https://creativecommons.org/licenses/by/4.0/"
	case c == "cc-by-sa":
		return "CC BY-SA 4.0", "https://creativecommons.org/licenses/by-sa/4.0/"
	case c == "cc-by-nc":
		return "CC BY-NC 4.0", "https://creativecommons.org/licenses/by-nc/4.0/"
	case c == "cc-by-nc-sa":
		return "CC BY-NC-SA 4.0", "https://creativecommons.org/licenses/by-nc-sa/4.0/"
	case c == "cc-by-nd":
		return "CC BY-ND 4.0", "https://creativecommons.org/licenses/by-nd/4.0/"
	case c == "cc-by-nc-nd":
		return "CC BY-NC-ND 4.0", "https://creativecommons.org/licenses/by-nc-nd/4.0/"
	}
	return code, ""
}

func periodLabel(a, b time.Time) string {
	switch {
	case a.Format("2006-01-02") == b.Format("2006-01-02"):
		return b.Format("January 2, 2006")
	case a.Year() != b.Year():
		return a.Format("Jan 2, 2006") + " – " + b.Format("Jan 2, 2006")
	case a.Month() != b.Month():
		return a.Format("Jan 2") + " – " + b.Format("Jan 2, 2006")
	}
	return a.Format("January 2") + " – " + b.Format("2, 2006")
}

func hourLabel(h int) string {
	switch {
	case h == 0:
		return "midnight"
	case h == 12:
		return "noon"
	case h < 12:
		return fmt.Sprintf("%d am", h)
	}
	return fmt.Sprintf("%d pm", h-12)
}

func joinNames(n []string) string {
	switch len(n) {
	case 0:
		return ""
	case 1:
		return n[0]
	case 2:
		return n[0] + " and " + n[1]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// reportServeHandler serves a built report like the CDN: files by path,
// index.html for anything else.
func reportServeHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil || fi.IsDir() {
			if p != "/" {
				r.URL.Path = "/"
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
}

// trimLeadingQuiet drops the days before the station first heard anything in
// the period (a station younger than the period), so the report covers only
// days it actually listened. It returns how many days it dropped.
func trimLeadingQuiet(d *reportData, first time.Time) int {
	skip := 0
	for skip < len(d.Daily)-1 && d.Daily[skip].Detections == 0 {
		skip++
	}
	if skip == 0 {
		return 0
	}
	d.Daily = d.Daily[skip:]
	for i := range d.Species {
		d.Species[i].Daily = d.Species[i].Daily[skip:]
	}
	start := first.AddDate(0, 0, skip)
	end, _ := time.ParseInLocation("2006-01-02", d.Period.End, start.Location())
	d.Period.Start = start.Format("2006-01-02")
	d.Period.Days = len(d.Daily)
	d.Period.Label = periodLabel(start, end)
	return skip
}
