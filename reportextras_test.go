package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// history is a station with a known past: sci -> detections on each date.
type history struct {
	names    map[string]string
	days     map[string]map[string]int
	rainDate string
}

func (h history) between(a, b string) map[string]int {
	out := map[string]int{}
	for day, m := range h.days {
		if day >= a && day <= b {
			for sci, n := range m {
				out[sci] += n
			}
		}
	}
	return out
}

func (h history) firstSeen(sci string) string {
	first := ""
	for day, m := range h.days {
		if m[sci] > 0 && (first == "" || day < first) {
			first = day
		}
	}
	return first
}

func (h history) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/v2/analytics/species/summary":
			var rows []map[string]any
			for sci, n := range h.between(q.Get("start_date"), q.Get("end_date")) {
				if n == 0 {
					continue
				}
				rows = append(rows, map[string]any{"scientific_name": sci, "common_name": h.names[sci], "species_code": sci[:3], "count": n,
					"first_heard": h.firstSeen(sci) + "T07:00:00-07:00", "last_heard": q.Get("end_date") + "T08:00:00-07:00", "avg_confidence": 0.8, "max_confidence": 0.9})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i]["scientific_name"].(string) < rows[j]["scientific_name"].(string) })
			writeJSON(w, rows)
		case r.URL.Path == "/api/v2/analytics/species/daily":
			rows := []map[string]any{}
			for sci, n := range h.days[q.Get("date")] {
				hc := make([]int, 24)
				hc[7] = n
				rows = append(rows, map[string]any{"scientific_name": sci, "count": n, "hourly_counts": hc})
			}
			writeJSON(w, rows)
		case r.URL.Path == "/api/v2/species":
			writeJSON(w, map[string]any{"rarity": map[string]any{"status": "common", "score": 0.9}, "taxonomy": map[string]any{"order": "Passeriformes", "family_common": "Finches"}})
		case r.URL.Path == "/api/v2/detections":
			writeJSON(w, map[string]any{"data": []any{}, "total": 0})
		case strings.HasPrefix(r.URL.Path, "/api/v2/weather/hourly/"):
			date := strings.TrimPrefix(r.URL.Path, "/api/v2/weather/hourly/")
			var rows []map[string]any
			for hr := 0; hr < 24; hr++ {
				main := "Clear"
				if date == h.rainDate && hr >= 9 && hr <= 14 {
					main = "Rain"
				}
				rows = append(rows, map[string]any{"time": fmt.Sprintf("%02d:00:00", hr), "temperature": 10 + float64(hr)/2, "wind_speed": 2.5, "clouds": nil, "weather_main": main, "weather_desc": strings.ToLower(main)})
			}
			writeJSON(w, map[string]any{"data": rows})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dayKey(loc *time.Location, back int) string {
	return time.Now().In(loc).AddDate(0, 0, -back).Format("2006-01-02")
}

func TestCompareWithTheWeekBefore(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	names := map[string]string{"Haemorhous mexicanus": "House Finch", "Sayornis nigricans": "Black Phoebe", "Corvus brachyrhynchos": "American Crow", "Melozone crissalis": "California Towhee"}
	days := map[string]map[string]int{}
	for back := 0; back <= 13; back++ {
		m := map[string]int{}
		if back <= 6 { // this week
			m["Haemorhous mexicanus"] = 30 // was 10 a day: up 200%
			m["Sayornis nigricans"] = 2    // was 10 a day: gone quiet
			m["Melozone crissalis"] = 3    // was not heard: new
		} else { // the week before
			m["Haemorhous mexicanus"] = 10
			m["Sayornis nigricans"] = 10
			m["Corvus brachyrhynchos"] = 5 // not heard now
		}
		days[dayKey(loc, back)] = m
	}
	srv := history{names: names, days: days}.server(t)

	rc := reportConfig{Days: 7}
	now := time.Now().In(loc)
	d, _, err := collectReport(context.Background(), config{BirdNET: srv.URL}, rc, loc, now.AddDate(0, 0, -6), now, now)
	if err != nil {
		t.Fatal(err)
	}
	c := d.Compare
	if c == nil || c.Label != "the week before" || c.ActiveDays != 7 || c.Detections != 175 || c.PrevPerDay != 25 || c.PerDay != 5*7 {
		t.Fatalf("compare %+v", c)
	}
	// 245 now over 175 before.
	if c.Change == nil || math.Abs(*c.Change-0.4) > 0.001 {
		t.Errorf("change %v", c.Change)
	}
	by := map[string]reportSpecies{}
	for _, sp := range d.Species {
		by[sp.Common] = sp
	}
	if f := by["House Finch"]; f.Prev != 70 || f.Change == nil || math.Abs(*f.Change-2) > 0.001 {
		t.Errorf("finch %+v change %v", f, f.Change)
	}
	if p := by["Black Phoebe"]; p.Change == nil || math.Abs(*p.Change+0.8) > 0.001 {
		t.Errorf("phoebe change %v", p.Change)
	}
	if tow := by["California Towhee"]; tow.Prev != 0 || tow.Change != nil {
		t.Errorf("a species not heard before has no change: %+v", tow)
	}
	if len(c.Quiet) != 1 || c.Quiet[0].Common != "Black Phoebe" || c.Quiet[0].Prev != 70 || c.Quiet[0].Now != 14 {
		t.Errorf("quiet %+v", c.Quiet)
	}
	if len(c.NotHeard) != 1 || c.NotHeard[0].Common != "American Crow" || c.NotHeard[0].Count != 35 {
		t.Errorf("not heard %+v", c.NotHeard)
	}
	text := strings.Join(d.Highlights, " | ")
	for _, want := range []string{"Activity is up 40% on the week before: 35 detections a listening day against 25.", "Biggest gain: House Finch, up 200%", "Gone quiet: Black Phoebe (down 80%).", "Heard the week before but not this time: American Crow (35)."} {
		if !strings.Contains(text, want) {
			t.Errorf("highlights lack %q:\n%s", want, text)
		}
	}

	// A station that was off most of the week before has nothing fair to compare with.
	young := map[string]map[string]int{}
	for back := 0; back <= 6; back++ {
		young[dayKey(loc, back)] = map[string]int{"Haemorhous mexicanus": 30}
	}
	young[dayKey(loc, 8)] = map[string]int{"Haemorhous mexicanus": 5}
	d2, _, err := collectReport(context.Background(), config{BirdNET: history{names: names, days: young}.server(t).URL}, rc, loc, now.AddDate(0, 0, -6), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if d2.Compare != nil || d2.Species[0].Change != nil || d2.Species[0].Prev != 0 {
		t.Errorf("compared with a period with one listening day: %+v", d2.Compare)
	}
}

func TestSpeciesGrowth(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	names := map[string]string{"Aaa aaa": "Bird A", "Bbb bbb": "Bird B", "Ccc ccc": "Bird C", "Ddd ddd": "Bird D", "Eee eee": "Bird E", "Fff fff": "Bird F"}
	days := map[string]map[string]int{
		dayKey(loc, 4): {"Aaa aaa": 5, "Bbb bbb": 5},
		dayKey(loc, 3): {"Aaa aaa": 5, "Ccc ccc": 5, "Ddd ddd": 5},
		dayKey(loc, 1): {"Eee eee": 5},
		dayKey(loc, 0): {"Fff fff": 5, "Aaa aaa": 5},
	}
	srv := history{names: names, days: days}.server(t)
	api := func(p string, v any) error {
		b, _, err := get(context.Background(), srv.Client(), srv.URL+"/api/v2"+p)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
	now := time.Now().In(loc)
	g, err := collectGrowth(context.Background(), api, now, loc)
	if err != nil || g == nil {
		t.Fatalf("growth %v %v", g, err)
	}
	if g.Total != 6 || len(g.Days) != 5 {
		t.Fatalf("growth %+v", g)
	}
	got := []string{}
	for _, d := range g.Days {
		got = append(got, fmt.Sprintf("%d/%d", d.New, d.Total))
	}
	// Bird A is heard again on day 2 and day 5, but is only new once.
	if strings.Join(got, " ") != "2/2 2/4 0/4 1/5 1/6" {
		t.Errorf("new/total by day: %v", got)
	}
	if first := g.Days[0]; first.Species[0] != "Bird A" || first.Species[1] != "Bird B" {
		t.Errorf("first day %+v", first)
	}
	// Too young for a curve: two days, three species.
	short := history{names: names, days: map[string]map[string]int{dayKey(loc, 1): {"Aaa aaa": 1, "Bbb bbb": 1}, dayKey(loc, 0): {"Ccc ccc": 1}}}.server(t)
	apiShort := func(p string, v any) error {
		b, _, err := get(context.Background(), short.Client(), short.URL+"/api/v2"+p)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
	if g, err := collectGrowth(context.Background(), apiShort, now, loc); g != nil || err != nil {
		t.Errorf("a young station has no curve: %+v %v", g, err)
	}
}

func TestWeatherAndSong(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	names := map[string]string{"Haemorhous mexicanus": "House Finch"}
	days := map[string]map[string]int{}
	for back := 0; back <= 3; back++ {
		days[dayKey(loc, back)] = map[string]int{"Haemorhous mexicanus": 40}
	}
	rain := dayKey(loc, 1)
	srv := history{names: names, days: days, rainDate: rain}.server(t)
	now := time.Now().In(loc)
	d, _, err := collectReport(context.Background(), config{BirdNET: srv.URL}, reportConfig{Days: 4, Weather: true}, loc, now.AddDate(0, 0, -3), now, now)
	if err != nil {
		t.Fatal(err)
	}
	w := d.Weather
	if w == nil || len(w.Hours) != 96 || w.Start != dayKey(loc, 3) {
		t.Fatalf("weather %+v", w)
	}
	if h := w.Hours[7]; h.N != 40 || h.Temp == nil || *h.Temp != 13.5 || h.Rain || h.Cloud != nil {
		t.Errorf("hour 7 of day 1: %+v", h)
	}
	if h := w.Hours[2*24+10]; !h.Rain || h.Cond != "Rain" {
		t.Errorf("rain hour: %+v", h)
	}
	if !strings.Contains(w.Insight, "Temperatures ranged from 10 °C to 22 °C.") {
		t.Errorf("insight %q", w.Insight)
	}
	// The singing is all at 7 am (before the rain at 9–14), so rain hours hear none.
	if !strings.Contains(w.Insight, "Rain quieted the station: 100% fewer detections an hour while it rained.") {
		t.Errorf("rain insight %q", w.Insight)
	}
	// Weather is optional and may be missing.
	if d, _, err := collectReport(context.Background(), config{BirdNET: srv.URL}, reportConfig{Days: 4}, loc, now.AddDate(0, 0, -3), now, now); err != nil || d.Weather != nil {
		t.Errorf("weather off: %v %+v", err, d.Weather)
	}
	dry := &reportWeather{Hours: make([]weatherHour, 48)}
	tp := 15.0
	for i := range dry.Hours {
		dry.Hours[i].Temp, dry.Hours[i].N = &tp, 5
	}
	if got := weatherInsight(dry); !strings.Contains(got, "It stayed dry.") {
		t.Errorf("dry: %q", got)
	}
	for i := 9; i < 14; i++ { // the birds sing through the rain
		dry.Hours[i].Rain, dry.Hours[i].N = true, 10
	}
	if got := weatherInsight(dry); !strings.Contains(got, "The birds sang through the rain: 100% more detections an hour than in the dry.") {
		t.Errorf("sang through: %q", got)
	}
}

func TestQuality(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	ds := noisyStation(now)
	for i := range ds {
		if ds[i].Common == "Great Horned Owl" {
			ds[i].Verified = "false_positive" // removed by a person
		}
	}
	d := &reportData{Species: []reportSpecies{{Code: "Cor", Scientific: "Corvus corax", Common: "Common Raven"}, {Code: "Hae", Scientific: "Haemorhous mexicanus", Common: "House Finch"}}}
	q := qualityOf(ds, d)
	if q == nil || q.Removed != 40 || q.Kept != 506 {
		t.Fatalf("quality %+v", q)
	}
	// 180 finches and a daytime hummingbird confirmed by two models, of 506.
	if q.TwoModel != round2(181.0/506) || q.Median != 0.4 || q.High != round2(181.0/506) {
		t.Errorf("shares %+v", q)
	}
	if len(q.Histogram) != 8 || q.Histogram[2] != 320 || q.Histogram[6] != 180+0 {
		t.Errorf("histogram %v", q.Histogram)
	}
	if len(q.Cautions) != 1 || q.Cautions[0].Code != "Cor" || q.Cautions[0].Reason != "mostly heard on one microphone" || q.Cautions[0].Count != 320 {
		t.Errorf("cautions %+v", q.Cautions)
	}
	if b, _ := json.Marshal(q); strings.Contains(string(b), "Garage") || strings.Contains(string(b), "Alley") {
		t.Errorf("a microphone name leaked into the report: %s", b)
	}
	if qualityOf(nil, d) != nil {
		t.Error("no detections, no quality")
	}
}

func TestSoundscape(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	series := func(band string, level func(hour int) float64) struct {
		Metric map[string]string `json:"metric"`
		Values [][2]any          `json:"values"`
	} {
		s := struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		}{Metric: map[string]string{"frequency_band": band}}
		start := time.Date(2026, 10, 25, 0, 0, 0, 0, loc)
		for i := 0; i < 48; i++ {
			ts := start.Add(time.Duration(i) * time.Hour)
			s.Values = append(s.Values, [2]any{float64(ts.Unix()), fmt.Sprint(level(ts.Hour()))})
		}
		return s
	}
	flat := func(int) float64 { return -60 }
	rush := func(h int) float64 { // low rumble loudest at 7, quietest at 3
		if h == 7 {
			return -40
		}
		if h == 3 {
			return -70
		}
		return -60
	}
	birds := func(h int) float64 { // 4 kHz peaks at 6 am, 12 dB above its quiet
		if h == 6 {
			return -48
		}
		return -60
	}
	result := []struct {
		Metric map[string]string `json:"metric"`
		Values [][2]any          `json:"values"`
	}{series("31.5_Hz", rush), series("125.0_Hz", rush), series("1.0_kHz", flat), series("4.0_kHz", birds), series("2.0_kHz", birds),
		series("1.2_kHz", flat) /* third-octave, not an octave */, series("junk", flat)}
	ss, err := soundscapeFrom(result, loc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ss.Bands, ",") != "31.5 Hz,125 Hz,1 kHz,2 kHz,4 kHz" || len(ss.Levels) != 5 {
		t.Fatalf("bands %v", ss.Bands)
	}
	if v := ss.Levels[0][7]; v == nil || *v != -40 || ss.Levels[0][12] == nil || ss.Min != -70 || ss.Max != -40 {
		t.Errorf("levels %v min %v max %v", ss.Levels[0], ss.Min, ss.Max)
	}
	if ss.Quietest != 3 || ss.Loudest != 7 {
		t.Errorf("quietest %d loudest %d", ss.Quietest, ss.Loudest)
	}
	for _, want := range []string{"quietest at 3 am and loudest at 7 am", "Between 2 and 8 kHz", "peaked at 6 am, 12 dB above its quietest hour"} {
		if !strings.Contains(ss.Insight, want) {
			t.Errorf("insight lacks %q: %s", want, ss.Insight)
		}
	}
	if _, err := soundscapeFrom(result[:2], loc); err == nil {
		t.Error("two bands are not a soundscape")
	}

	// Through Prometheus' HTTP API, with basic auth and the configured metric.
	var gotQuery, gotUser, gotPass string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		gotUser, gotPass, _ = r.BasicAuth()
		resp := map[string]any{"status": "success", "data": map[string]any{"result": []any{}}}
		var res []map[string]any
		for _, s := range result {
			res = append(res, map[string]any{"metric": s.Metric, "values": s.Values})
		}
		resp["data"] = map[string]any{"result": res}
		writeJSON(w, resp)
	}))
	defer prom.Close()
	d := &reportData{Period: reportPeriod{Start: "2026-10-25"}}
	rc := reportConfig{Prometheus: prom.URL + "/", PrometheusUser: "reader", PrometheusPass: "s3cret", SoundMetric: "my_level"}
	got, err := collectSoundscape(context.Background(), rc, d, loc, time.Date(2026, 10, 26, 23, 0, 0, 0, loc))
	if err != nil || got == nil {
		t.Fatalf("collect: %v", err)
	}
	if !strings.Contains(gotQuery, "avg_over_time(my_level[1h])") || gotUser != "reader" || gotPass != "s3cret" {
		t.Errorf("query %q user %q pass %q", gotQuery, gotUser, gotPass)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 401) }))
	defer down.Close()
	if _, err := collectSoundscape(context.Background(), reportConfig{Prometheus: down.URL}, d, loc, time.Now()); err == nil {
		t.Error("an unreachable Prometheus must be an error, not a blank soundscape")
	}
}

func TestSpeciesClips(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	today := time.Now().In(loc).Format("2006-01-02")
	srv := fakeAnalytics(t, today)
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "report")
	c := config{BirdNET: srv.URL, Out: out, SkipAudio: []string{"doorbell"}}
	if err := buildReport(context.Background(), c, reportConfig{Days: 7, SpeciesClips: 2}); err != nil {
		t.Fatal(err)
	}
	var d reportData
	readJSON(t, filepath.Join(out, "data", "report.json"), &d)
	byName := map[string]reportSpecies{}
	for _, sp := range d.Species {
		byName[sp.Common] = sp
	}
	clipID := func(cl reportClip) string {
		b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(cl.Audio)))
		id, _, _ := strings.Cut(strings.TrimPrefix(string(b), "fLaC-"), "-")
		return id
	}
	ids := func(cs []reportClip) string {
		var s []string
		for _, cl := range cs {
			s = append(s, clipID(cl))
		}
		return strings.Join(s, ",")
	}
	// Finch: 13 (two models, 0.95) then 14 (0.95), never the doorbell's 11 or the clipless 20.
	if got := ids(byName["House Finch"].Clips); got != "13,14" {
		t.Errorf("finch clips %s", got)
	}
	// Owl: the false positive 12 is out; 9 (0.96) then 18 (0.4).
	if got := ids(byName["Great Horned Owl"].Clips); got != "9,18" {
		t.Errorf("owl clips %s", got)
	}
	if byName["House Finch"].Clips[0].Spectrogram == "" {
		t.Error("clip without its spectrogram")
	}
	// -species-clips 0 publishes none.
	if err := buildReport(context.Background(), c, reportConfig{Days: 7}); err != nil {
		t.Fatal(err)
	}
	var none reportData
	readJSON(t, filepath.Join(out, "data", "report.json"), &none)
	for _, sp := range none.Species {
		if len(sp.Clips) > 0 {
			t.Errorf("clips with -species-clips 0: %+v", sp.Clips)
		}
	}
}
