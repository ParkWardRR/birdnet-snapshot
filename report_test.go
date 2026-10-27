package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeAnalytics serves the BirdNET-Go endpoints the report reads.
// earlier are further days the station was listening (it is one day old without them).
func fakeAnalytics(t *testing.T, today string, earlier ...string) *httptest.Server {
	t.Helper()
	hourly := func(peak, n int) []int {
		h := make([]int, 24)
		h[peak] = n
		return h
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v2/analytics/species/summary":
			writeJSON(w, []map[string]any{
				{"scientific_name": "Bubo virginianus", "common_name": "Great Horned Owl", "species_code": "grhowl", "count": 120,
					"first_heard": today + "T01:02:03-07:00", "last_heard": today + "T05:00:00-07:00", "avg_confidence": 0.61, "max_confidence": 0.97},
				{"scientific_name": "Haemorhous mexicanus", "common_name": "House Finch", "species_code": "houfin", "count": 40,
					"first_heard": today + "T07:10:00-07:00", "last_heard": today + "T08:00:00-07:00", "avg_confidence": 0.7, "max_confidence": 0.9},
				{"scientific_name": "Ammospiza nelsoni", "common_name": "Nelson's Sparrow", "species_code": "nstspa", "count": 1,
					"first_heard": today + "T07:30:00-07:00", "last_heard": today + "T07:30:00-07:00", "avg_confidence": 0.5, "max_confidence": 0.5},
			})
		case "/api/v2/analytics/species/daily":
			if d := q.Get("date"); d != today && !slices.Contains(earlier, d) {
				writeJSON(w, []any{})
				return
			}
			writeJSON(w, []map[string]any{
				{"scientific_name": "Bubo virginianus", "count": 120, "hourly_counts": hourly(2, 120), "is_new_species": true},
				{"scientific_name": "Haemorhous mexicanus", "count": 40, "hourly_counts": hourly(7, 40)},
				{"scientific_name": "Ammospiza nelsoni", "count": 1, "hourly_counts": hourly(7, 1)},
			})
		case "/api/v2/species":
			status, score := "very_common", 0.98
			if q.Get("scientific_name") == "Ammospiza nelsoni" {
				status, score = "rare", 0.05
			}
			writeJSON(w, map[string]any{
				"rarity":   map[string]any{"status": status, "score": score, "latitude": 34.03, "longitude": -118.47},
				"taxonomy": map[string]any{"order": "Strigiformes", "family_common": "Owls"},
			})
		case "/api/v2/media/species-image":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(append([]byte("jpeg-"), q.Get("name")...))
		case "/api/v2/media/species-image/info":
			writeJSON(w, map[string]any{"authorName": "A. Photographer", "licenseName": "CC0", "sourceProvider": "avicommons"})
		case "/api/v2/analytics/time/dawn-onset":
			// Only the daytime singer is asked about: the owl (night caller) and the
			// one-off sparrow must never be requested.
			if q.Get("species") != "House Finch" {
				http.Error(w, "unexpected species "+q.Get("species"), http.StatusInternalServerError)
				return
			}
			var out []map[string]any
			for d, _ := time.Parse("2006-01-02", q.Get("start_date")); !d.After(mustDate(q.Get("end_date"))); d = d.AddDate(0, 0, 1) {
				min := 25
				switch d.Format("2006-01-02") {
				case today:
					min = 30
				case earlier[len(earlier)-1]:
					min = 20
				}
				out = append(out, map[string]any{"date": d.Format("2006-01-02"), "onsetRelMinutes": float64(min) + 0.4, "detectionCount": 40})
			}
			writeJSON(w, out)
		case "/api/v2/detections/recent":
			writeJSON(w, []map[string]any{{"timestamp": today + "T07:30:00-07:00"}})
		case "/api/v2/detections":
			dets := reportDetections(today)
			if q.Get("sortBy") == "confidence_desc" {
				sort.SliceStable(dets, func(i, j int) bool { return dets[i]["confidence"].(float64) > dets[j]["confidence"].(float64) })
			} else {
				sort.SliceStable(dets, func(i, j int) bool { return dets[i]["id"].(int) > dets[j]["id"].(int) })
			}
			if q.Get("offset") != "0" {
				dets = nil
			}
			writeJSON(w, map[string]any{"data": dets, "total": len(dets)})
		default:
			if id, ok := strings.CutPrefix(r.URL.Path, "/api/v2/audio/"); ok && id != "20" {
				w.Header().Set("Content-Type", "audio/flac")
				_, _ = w.Write([]byte("fLaC-" + id + "-" + strings.Repeat("x", 2048)))
				return
			}
			if id, ok := strings.CutPrefix(r.URL.Path, "/api/v2/spectrogram/"); ok && id != "16" {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("png-" + id))
				return
			}
			http.NotFound(w, r)
		}
	}))
}

// reportDetections covers every reason a clip is or is not published.
func reportDetections(today string) []map[string]any {
	det := func(id int, sci, common, src string, conf float64, models int, verified, clip string) map[string]any {
		m := make([]map[string]any, models)
		for i := range m {
			m[i] = map[string]any{"modelName": fmt.Sprint("m", i)}
		}
		return map[string]any{
			"id": id, "timestamp": fmt.Sprintf("%sT07:%02d:00-07:00", today, id), "scientificName": sci, "commonName": common,
			"speciesCode": "x", "confidence": conf, "clipName": clip, "verified": verified, "modelContributions": m,
			"source": map[string]any{"id": "rtsp://cam/" + strings.ToLower(src), "displayName": src},
		}
	}
	const finch, owl, sparrow = "Haemorhous mexicanus", "Bubo virginianus", "Ammospiza nelsoni"
	return []map[string]any{
		det(11, finch, "House Finch", "Doorbell", 0.99, 3, "unverified", "a.flac"),                // private source
		det(12, owl, "Great Horned Owl", "Alley", 0.98, 3, "false_positive", "a.flac"),            // marked wrong
		det(17, "Turdus migratorius", "American Robin", "Alley", 0.97, 3, "unverified", "a.flac"), // not in the report
		det(14, finch, "House Finch", "Alley", 0.95, 1, "unverified", "a.flac"),                   // tie, fewer models
		det(13, finch, "House Finch", "Alley", 0.95, 2, "correct", "a.flac"),                      // best 1
		det(9, owl, "Great Horned Owl", "Alley", 0.96, 1, "unverified", "a.flac"),                 // best 2: one model, so after 13
		det(15, sparrow, "Nelson's Sparrow", "Alley", 0.6, 1, "unverified", ""),                   // no clip
		det(16, sparrow, "Nelson's Sparrow", "Garage", 0.5, 1, "unverified", "a.flac"),            // best 3, no spectrogram
		det(18, owl, "Great Horned Owl", "Garage", 0.4, 1, "unverified", "a.flac"),                // owl taken by 9
		det(20, finch, "House Finch", "Alley", 0.35, 1, "unverified", "a.flac"),                   // newest; clip not written yet
		det(21, owl, "Great Horned Owl", "Alley", 0.3, 1, "unverified", "a.flac"),                 // recent 1
		det(19, finch, "House Finch", "Alley", 0.3, 1, "unverified", "a.flac"),                    // recent 2 (18 is used)
		det(10, sparrow, "Nelson's Sparrow", "Alley", 0.3, 1, "unverified", "a.flac"),             // recent 3
	}
}

func TestBuildReport(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	today := time.Now().In(loc).Format("2006-01-02")
	srv := fakeAnalytics(t, today)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "report")
	c := config{BirdNET: srv.URL, Out: out, SkipAudio: []string{"doorbell"}}
	rc := reportConfig{Title: "Field Notes", Subtitle: "s", Station: "A station", Days: 7, Clips: 3}
	if err := buildReport(context.Background(), c, rc); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "robots.txt", "data/report.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(out, "data", "report.json"))
	if strings.Contains(string(raw), "34.03") || strings.Contains(string(raw), "118.47") || strings.Contains(string(raw), "latitude") {
		t.Errorf("coordinates copied into the report: %s", raw)
	}
	var d reportData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	// A one-day-old station: the period shrinks to the days it listened.
	if d.Period.Days != 1 || d.Period.Start != today || len(d.Daily) != 1 {
		t.Errorf("period not trimmed to listened days: %+v daily=%d", d.Period, len(d.Daily))
	}
	if d.Totals.Detections != 161 || d.Totals.Species != 3 || d.Totals.BusiestHour != 2 || d.Totals.NewSpecies != 1 {
		t.Errorf("totals %+v", d.Totals)
	}
	owl := d.Species[0]
	if owl.Common != "Great Horned Owl" || owl.PeakHour != 2 || owl.NightShare != 1 || owl.Family != "Owls" || owl.Rarity != "very common" {
		t.Errorf("owl %+v", owl)
	}
	if owl.Photo == nil || !strings.HasPrefix(owl.Photo.Large, "/_snap/") || owl.Photo.Author != "A. Photographer" {
		t.Errorf("photo %+v", owl.Photo)
	} else if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(owl.Photo.Large))); err != nil {
		t.Errorf("photo file missing: %v", err)
	}
	joined := strings.Join(d.Highlights, " | ")
	for _, want := range []string{"Great Horned Owl led the period", "night shift", "Nelson's Sparrow, rare", "New to the station: Great Horned Owl"} {
		if !strings.Contains(joined, want) {
			t.Errorf("highlights lack %q: %s", want, joined)
		}
	}
	if d.PDF != "" {
		t.Error("PDF was off")
	}

	// Recordings: the private source, the false positive, the clipless, the
	// unlisted species and the unwritten clip are passed over; one per species.
	if d.Recordings == nil {
		t.Fatal("no recordings")
	}
	clipID := func(r reportClip) string {
		b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(r.Audio)))
		if err != nil {
			return "missing"
		}
		id, _, _ := strings.Cut(strings.TrimPrefix(string(b), "fLaC-"), "-")
		return id
	}
	ids := func(rs []reportClip) string {
		var s []string
		for _, r := range rs {
			s = append(s, clipID(r))
		}
		return strings.Join(s, ",")
	}
	if got := ids(d.Recordings.Best); got != "13,9,16" {
		t.Errorf("best clips %s, want 13,9,16", got)
	}
	if got := ids(d.Recordings.Recent); got != "21,19,10" {
		t.Errorf("recent clips %s, want 21,19,10", got)
	}
	b0 := d.Recordings.Best[0]
	if b0.Common != "House Finch" || b0.Models != 2 || b0.Confidence != 0.95 || !strings.HasSuffix(b0.Audio, ".flac") || !strings.HasSuffix(b0.Spectrogram, ".png") {
		t.Errorf("best clip %+v", b0)
	}
	if d.Recordings.Best[2].Spectrogram != "" {
		t.Errorf("spectrogram that 404s should be left out: %+v", d.Recordings.Best[2])
	}
	if strings.Contains(string(raw), "Doorbell") || strings.Contains(string(raw), "rtsp") || strings.Contains(string(raw), "Garage") {
		t.Errorf("source names leaked into the report: %s", raw)
	}

	// A confidence floor drops the weak clips from both lists.
	rc.ClipMin = 0.45
	if err := buildReport(context.Background(), c, rc); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(out, "data", "report.json"), &d)
	if b, r := ids(d.Recordings.Best), ids(d.Recordings.Recent); b != "13,9,16" || r != "14" {
		t.Errorf("with -clip-min 0.45: best %s recent %s, want 13,9,16 and 14", b, r)
	}

	// -clips 0 and -no-audio publish none.
	for _, cc := range []struct {
		c  config
		rc reportConfig
	}{
		{config{BirdNET: srv.URL, Out: out}, reportConfig{Days: 7}},
		{config{BirdNET: srv.URL, Out: out, NoAudio: true}, reportConfig{Days: 7, Clips: 3}},
	} {
		if err := buildReport(context.Background(), cc.c, cc.rc); err != nil {
			t.Fatal(err)
		}
		var d2 reportData
		readJSON(t, filepath.Join(out, "data", "report.json"), &d2)
		if d2.Recordings != nil {
			t.Errorf("recordings published with clips=%d noAudio=%v", cc.rc.Clips, cc.c.NoAudio)
		}
		if m, _ := filepath.Glob(filepath.Join(out, "_snap", "*.flac")); len(m) > 0 {
			t.Errorf("audio files written: %v", m)
		}
	}
}

func TestBuildReportRefusesEmptyPeriod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/analytics/species/summary" {
			writeJSON(w, []any{})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	err := buildReport(context.Background(), config{BirdNET: srv.URL, Out: t.TempDir() + "/r"}, reportConfig{Days: 7})
	if err == nil || !strings.Contains(err.Error(), "no detections") {
		t.Errorf("want refusal, got %v", err)
	}
}

func TestReportPDF(t *testing.T) {
	chromeOrSkip(t)
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	srv := fakeAnalytics(t, time.Now().In(loc).Format("2006-01-02"))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "report")
	c := config{BirdNET: srv.URL, Out: out, ChromePath: os.Getenv("CHROME_PATH"), NoSandbox: envBool("CHROME_NO_SANDBOX", false)}
	if err := buildReport(context.Background(), c, reportConfig{Title: "Field Notes", Days: 7, PDF: true}); err != nil {
		t.Fatal(err)
	}
	var d reportData
	readJSON(t, filepath.Join(out, "data", "report.json"), &d)
	pdf, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(d.PDF)))
	if err != nil {
		t.Fatalf("pdf %q: %v", d.PDF, err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-") || len(pdf) < 20_000 {
		t.Errorf("not a real PDF: %d bytes", len(pdf))
	}
}

func TestReportHelpers(t *testing.T) {
	la, _ := time.LoadLocation("America/Los_Angeles")
	d := func(s string) time.Time { tt, _ := time.ParseInLocation("2006-01-02", s, la); return tt }
	for _, tc := range []struct{ a, b, want string }{
		{"2026-10-18", "2026-10-24", "October 18 – 24, 2026"},
		{"2026-09-28", "2026-10-04", "Sep 28 – Oct 4, 2026"},
		{"2026-12-29", "2027-01-04", "Dec 29, 2026 – Jan 4, 2027"},
		{"2026-10-24", "2026-10-24", "October 24, 2026"},
	} {
		if got := periodLabel(d(tc.a), d(tc.b)); got != tc.want {
			t.Errorf("periodLabel(%s,%s) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
	if commas(1234567) != "1,234,567" || commas(12) != "12" {
		t.Error("commas")
	}
	if hourLabel(0) != "midnight" || hourLabel(12) != "noon" || hourLabel(7) != "7 am" || hourLabel(19) != "7 pm" {
		t.Error("hourLabel")
	}
	if joinNames([]string{"a", "b", "c"}) != "a, b and c" || joinNames([]string{"a", "b"}) != "a and b" {
		t.Error("joinNames")
	}
	if l, u := licenseInfo("cc-by-nc"); l != "CC BY-NC 4.0" || !strings.Contains(u, "by-nc") {
		t.Errorf("licenseInfo cc-by-nc = %s %s", l, u)
	}
	if l, _ := licenseInfo("CC0 3.0"); l != "CC0" {
		t.Errorf("licenseInfo CC0 = %s", l)
	}
	for h := 0; h < 24; h++ {
		if isNight(h) != (h >= 19 || h < 6) {
			t.Errorf("isNight(%d)", h)
		}
	}
}

func mustDate(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestOnsetDays(t *testing.T) {
	la, _ := time.LoadLocation("America/Los_Angeles")
	at := func(s string) time.Time {
		tt, err := time.ParseInLocation("2006-01-02 15:04", s, la)
		if err != nil {
			t.Fatal(err)
		}
		return tt
	}
	f := func(v float64) *float64 { return &v }
	days := []reportDay{{Date: "2026-10-23"}, {Date: "2026-10-24"}, {Date: "2026-10-25"}, {Date: "2026-10-26"}}
	raw := []onsetPoint{{"2026-10-23", f(101), 84}, {"2026-10-24", f(30.4), 204}, {"2026-10-25", nil, 3}, {"2026-10-26", f(-19.6), 77}}
	show := func(ds []reportOnsetDay) string {
		var s []string
		for _, d := range ds {
			if d.Minutes == nil {
				s = append(s, "-")
			} else {
				s = append(s, fmt.Sprint(*d.Minutes))
			}
		}
		return strings.Join(s, " ")
	}
	for _, tc := range []struct {
		name         string
		start, now   string
		want         string
		wantCountSum int
	}{
		// The station's first bird was heard at 10:54, after dawn: the 23rd's
		// onset is when listening began. The server's own null stays null.
		{"started after dawn", "2026-10-23 10:54", "2026-10-26 14:00", "- 30 - -20", 368},
		{"started before dawn", "2026-10-23 05:10", "2026-10-26 14:00", "101 30 - -20", 368},
		// 7:30 am on the 26th: that morning's chorus isn't over.
		{"today unfinished", "2026-10-23 05:10", "2026-10-26 07:30", "101 30 - -", 368},
		{"no station start known", "0001-01-01 00:00", "2026-10-26 14:00", "101 30 - -20", 368},
	} {
		start := at(tc.start)
		if tc.start[:4] == "0001" {
			start = time.Time{}
		}
		ds := onsetDays(days, raw, start, at(tc.now))
		if got := show(ds); got != tc.want {
			t.Errorf("%s: onsets %q, want %q", tc.name, got, tc.want)
		}
		sum := 0
		for _, d := range ds {
			sum += d.Count
		}
		if sum != tc.wantCountSum {
			t.Errorf("%s: counts %d, want %d", tc.name, sum, tc.wantCountSum)
		}
	}
	// A day the server left out stays in the series, empty.
	if ds := onsetDays(days, raw[:2], at("2026-10-23 05:10"), at("2026-10-26 14:00")); len(ds) != 4 || ds[3].Minutes != nil {
		t.Errorf("missing day: %+v", ds)
	}
	if got := earliestHeard([]reportSpecies{{FirstHeard: "2026-10-24T07:00:00-07:00"}, {FirstHeard: "2026-10-23T10:54:00-07:00"}, {FirstHeard: "junk"}}, la); got.Format("2006-01-02 15:04") != "2026-10-23 10:54" {
		t.Errorf("earliestHeard = %v", got)
	}
}

func TestBuildReportDawn(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	now := time.Now().In(loc)
	today, d1, d2 := now.Format("2006-01-02"), now.AddDate(0, 0, -1).Format("2006-01-02"), now.AddDate(0, 0, -2).Format("2006-01-02")
	srv := fakeAnalytics(t, today, d1, d2)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "report")
	if err := buildReport(context.Background(), config{BirdNET: srv.URL, Out: out}, reportConfig{Days: 7, Dawn: true}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, "data", "report.json"))
	var d reportData
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	if d.Dawn == nil || len(d.Dawn.Series) != 1 || d.Dawn.Series[0].Common != "House Finch" || d.Dawn.Series[0].Code != "houfin" {
		t.Fatalf("dawn %+v", d.Dawn)
	}
	days := d.Dawn.Series[0].Days
	if len(days) != 3 || days[0].Date != d2 || days[1].Date != d1 || days[2].Date != today {
		t.Fatalf("days %+v", days)
	}
	if days[0].Minutes == nil || *days[0].Minutes != 20 || days[1].Minutes == nil || *days[1].Minutes != 25 {
		t.Errorf("onsets %+v %+v", days[0].Minutes, days[1].Minutes)
	}
	if settled := now.Hour() >= dawnSettledHour; settled != (days[2].Minutes != nil) {
		t.Errorf("today's onset at %d:00: %v", now.Hour(), days[2].Minutes)
	}
	// Only offsets: no dawn time, and nothing that looks like a coordinate.
	for _, bad := range []string{"civil", "sunrise", "latitude", "longitude", "34.03"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("report.json contains %q", bad)
		}
	}

	// -dawn=false, and a BirdNET-Go without the endpoint, leave the chart out.
	if err := buildReport(context.Background(), config{BirdNET: srv.URL, Out: out}, reportConfig{Days: 7}); err != nil {
		t.Fatal(err)
	}
	var d2r reportData
	readJSON(t, filepath.Join(out, "data", "report.json"), &d2r)
	if d2r.Dawn != nil {
		t.Error("dawn charted with -dawn off")
	}
	// One-day station: nothing to compare.
	one := fakeAnalytics(t, today)
	defer one.Close()
	if err := buildReport(context.Background(), config{BirdNET: one.URL, Out: out}, reportConfig{Days: 7, Dawn: true}); err != nil {
		t.Fatal(err)
	}
	readJSON(t, filepath.Join(out, "data", "report.json"), &d2r)
	if d2r.Dawn != nil {
		t.Errorf("dawn charted from one day: %+v", d2r.Dawn)
	}
}
