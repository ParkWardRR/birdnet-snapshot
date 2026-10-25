package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAnalytics serves the BirdNET-Go endpoints the report reads.
func fakeAnalytics(t *testing.T, today string) *httptest.Server {
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
			if q.Get("date") != today { // the station is one day old
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
		case "/api/v2/detections/recent":
			writeJSON(w, []map[string]any{{"timestamp": today + "T07:30:00-07:00"}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestBuildReport(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	today := time.Now().In(loc).Format("2006-01-02")
	srv := fakeAnalytics(t, today)
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "report")
	c := config{BirdNET: srv.URL, Out: out}
	rc := reportConfig{Title: "Field Notes", Subtitle: "s", Station: "A station", Days: 7}
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
