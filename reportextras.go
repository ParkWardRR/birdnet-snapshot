package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Week over week

// reportCompare is how the period stacks up against the one before it.
type reportCompare struct {
	Label      string          `json:"label"` // "the week before"
	Start      string          `json:"start"`
	End        string          `json:"end"`
	Detections int             `json:"detections"`
	Species    int             `json:"species"`
	ActiveDays int             `json:"activeDays"`
	PerDay     int             `json:"perDay"`     // detections a day heard, now
	PrevPerDay int             `json:"prevPerDay"` // and then
	Change     *float64        `json:"change,omitempty"`
	NotHeard   []reportMissing `json:"notHeard,omitempty"` // heard then, not now
	Quiet      []reportQuiet   `json:"quiet,omitempty"`    // regulars that dropped away
}

type reportMissing struct {
	Code   string `json:"code"`
	Common string `json:"common"`
	Count  int    `json:"count"`
}

type reportQuiet struct {
	Code   string  `json:"code"`
	Common string  `json:"common"`
	Prev   int     `json:"prev"`
	Now    int     `json:"now"`
	Change float64 `json:"change"`
}

const (
	compareMinDays   = 3  // listening days the earlier period needs
	compareMinCount  = 10 // detections before a species' change is quoted
	compareQuietMin  = 30 // a regular: at least this many before
	compareQuietDrop = 0.5
)

// collectCompare fills d.Compare and each species' Prev and Change from the
// days days before first. Rates are per listening day, so a station that was
// off for part of either period is not misread. It returns an error when there
// is nothing fair to compare with.
func collectCompare(ctx context.Context, api func(string, any) error, d *reportData, first time.Time, days int) error {
	pFirst, pEnd := first.AddDate(0, 0, -days), first.AddDate(0, 0, -1)
	var prev []struct {
		Sci    string `json:"scientific_name"`
		Common string `json:"common_name"`
		Code   string `json:"species_code"`
		Count  int    `json:"count"`
	}
	if err := api(fmt.Sprintf("/analytics/species/summary?start_date=%s&end_date=%s", pFirst.Format("2006-01-02"), pEnd.Format("2006-01-02")), &prev); err != nil {
		return err
	}
	active, total := 0, 0
	for i := 0; i < days; i++ {
		var rows []struct {
			Count int `json:"count"`
		}
		if err := api("/analytics/species/daily?date="+pFirst.AddDate(0, 0, i).Format("2006-01-02")+"&limit=1000", &rows); err != nil {
			return err
		}
		sum := 0
		for _, r := range rows {
			sum += r.Count
		}
		if sum > 0 {
			active++
		}
		total += sum
	}
	if active < compareMinDays || d.Totals.ActiveDays < 1 {
		return fmt.Errorf("the period before has %d listening days", active)
	}
	label := "the week before"
	if days != 7 {
		label = fmt.Sprintf("the %d days before", days)
	}
	cmp := &reportCompare{
		Label: label, Start: pFirst.Format("2006-01-02"), End: pEnd.Format("2006-01-02"),
		Detections: total, Species: len(prev), ActiveDays: active,
		PerDay:     int(math.Round(float64(d.Totals.Detections) / float64(d.Totals.ActiveDays))),
		PrevPerDay: int(math.Round(float64(total) / float64(active))),
	}
	if total > 0 {
		ch := round2(float64(d.Totals.Detections)/float64(d.Totals.ActiveDays)/(float64(total)/float64(active)) - 1)
		cmp.Change = &ch
	}
	before := map[string]int{}
	for _, p := range prev {
		before[p.Sci] = p.Count
	}
	now := map[string]bool{}
	for i := range d.Species {
		sp := &d.Species[i]
		now[sp.Scientific] = true
		p := before[sp.Scientific]
		sp.Prev = p
		if p > 0 && (p >= compareMinCount || sp.Count >= compareMinCount) {
			ch := round2((float64(sp.Count)/float64(d.Totals.ActiveDays))/(float64(p)/float64(active)) - 1)
			sp.Change = &ch
			if p >= compareQuietMin && ch <= -compareQuietDrop {
				cmp.Quiet = append(cmp.Quiet, reportQuiet{Code: sp.Code, Common: sp.Common, Prev: p, Now: sp.Count, Change: ch})
			}
		}
	}
	for _, p := range prev {
		if !now[p.Sci] && p.Count >= compareMinCount {
			cmp.NotHeard = append(cmp.NotHeard, reportMissing{Code: p.Code, Common: p.Common, Count: p.Count})
		}
	}
	sort.Slice(cmp.NotHeard, func(i, j int) bool { return cmp.NotHeard[i].Count > cmp.NotHeard[j].Count })
	sort.Slice(cmp.Quiet, func(i, j int) bool { return cmp.Quiet[i].Change < cmp.Quiet[j].Change })
	if len(cmp.NotHeard) > 6 {
		cmp.NotHeard = cmp.NotHeard[:6]
	}
	if len(cmp.Quiet) > 6 {
		cmp.Quiet = cmp.Quiet[:6]
	}
	d.Compare = cmp
	return nil
}

func changeText(ch float64) string {
	if ch >= 0 {
		return fmt.Sprintf("up %d%%", int(math.Round(ch*100)))
	}
	return fmt.Sprintf("down %d%%", int(math.Round(-ch*100)))
}

// compareHeadline is the one sentence about overall activity.
func compareHeadline(d *reportData) []string {
	c := d.Compare
	if c == nil || c.Change == nil || math.Abs(*c.Change) < 0.05 {
		return nil
	}
	return []string{fmt.Sprintf("Activity is %s on %s: %s detections a listening day against %s.",
		changeText(*c.Change), c.Label, commas(c.PerDay), commas(c.PrevPerDay))}
}

// compareNotes are the lines about who gained, went quiet or went missing.
func compareNotes(d *reportData) []string {
	c := d.Compare
	if c == nil {
		return nil
	}
	var out []string
	var best *reportSpecies
	for i := range d.Species {
		sp := &d.Species[i]
		if sp.Change != nil && sp.Count >= 30 && *sp.Change >= 0.25 && (best == nil || *sp.Change > *best.Change) {
			best = sp
		}
	}
	if best != nil {
		out = append(out, fmt.Sprintf("Biggest gain: %s, %s on %s.", best.Common, changeText(*best.Change), c.Label))
	}
	if len(c.Quiet) > 0 {
		var parts []string
		for i, q := range c.Quiet {
			if i == 3 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", q.Common, changeText(q.Change)))
		}
		out = append(out, "Gone quiet: "+joinNames(parts)+".")
	}
	if len(c.NotHeard) > 0 {
		var parts []string
		for i, m := range c.NotHeard {
			if i == 3 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%d)", m.Common, m.Count))
		}
		out = append(out, fmt.Sprintf("Heard %s but not this time: %s.", c.Label, joinNames(parts)))
	}
	return out
}

// ---------------------------------------------------------------------------
// Quality

// reportQuality says how much the period's detections can be trusted: how many
// had two models agreeing, how confident they were, how many a person threw
// out, and which species deserve a pinch of salt.
type reportQuality struct {
	Kept      int             `json:"kept"`
	Removed   int             `json:"removed"` // marked false positive by a person, and left out of everything
	TwoModel  float64         `json:"twoModel"`
	High      float64         `json:"high"` // confidence of 0.8 or more
	Median    float64         `json:"median"`
	Histogram []int           `json:"histogram"` // detections per 0.1 of confidence, 0.2 to 1.0
	Cautions  []reportCaution `json:"cautions,omitempty"`
}

type reportCaution struct {
	Code     string  `json:"code"`
	Common   string  `json:"common"`
	Count    int     `json:"count"`
	Mean     float64 `json:"mean"`
	TwoModel float64 `json:"twoModel"`
	Reason   string  `json:"reason"`
}

// qualityOf reads the period's detections. No microphone names are copied:
// reasons only say "one microphone".
func qualityOf(dets []apiDetection, d *reportData) *reportQuality {
	st := summarise(dets)
	if st.Kept == 0 {
		return nil
	}
	q := &reportQuality{Kept: st.Kept, Removed: st.Removed, Histogram: make([]int, 8)}
	var confs []float64
	two, high := 0, 0
	for _, x := range dets {
		if x.isRemoved() {
			continue
		}
		confs = append(confs, x.Confidence)
		if x.multi() {
			two++
		}
		if x.Confidence >= 0.8 {
			high++
		}
		b := int(x.Confidence*10) - 2
		q.Histogram[max(0, min(7, b))]++
	}
	sort.Float64s(confs)
	q.Median = round2(confs[len(confs)/2])
	q.TwoModel = round2(share(two, st.Kept))
	q.High = round2(share(high, st.Kept))

	codes := map[string]string{}
	for _, sp := range d.Species {
		codes[sp.Scientific] = sp.Code
	}
	for _, s := range st.ranked() {
		if s.N < 30 || codes[s.Sci] == "" || (s.meanConf() >= 0.55 && s.multiShare() >= 0.4) {
			continue
		}
		reason := "low confidence overall"
		if _, n := s.topSource(); len(st.BySource) >= 2 && share(n, s.N) >= 0.7 {
			reason = "mostly heard on one microphone"
		} else if s.multiShare() < 0.4 {
			reason = "few confirmed by a second model"
		}
		q.Cautions = append(q.Cautions, reportCaution{Code: codes[s.Sci], Common: s.Common, Count: s.N,
			Mean: round2(s.meanConf()), TwoModel: round2(s.multiShare()), Reason: reason})
		if len(q.Cautions) == 5 {
			break
		}
	}
	return q
}

// ---------------------------------------------------------------------------
// Species growth

// reportGrowth is how the station's species list has grown: one point a day.
type reportGrowth struct {
	Days  []growthDay `json:"days"`
	Total int         `json:"total"`
}

type growthDay struct {
	Date    string   `json:"date"`
	New     int      `json:"new"`
	Total   int      `json:"total"`
	Species []string `json:"species,omitempty"` // first heard that day (the first few)
}

const growthMaxDays = 365

// collectGrowth builds the species accumulation curve from the time each
// species was first heard (false positives are not counted by BirdNET-Go). It
// returns nil for a station too young to have a curve.
func collectGrowth(ctx context.Context, api func(string, any) error, end time.Time, loc *time.Location) (*reportGrowth, error) {
	var rows []struct {
		Common string `json:"common_name"`
		First  string `json:"first_heard"`
	}
	if err := api("/analytics/species/summary?start_date=2000-01-01&end_date="+end.Format("2006-01-02"), &rows); err != nil {
		return nil, err
	}
	byDay := map[string][]string{}
	var earliest string
	for _, r := range rows {
		t, err := time.Parse(time.RFC3339, r.First)
		if err != nil {
			continue
		}
		day := t.In(loc).Format("2006-01-02")
		byDay[day] = append(byDay[day], r.Common)
		if earliest == "" || day < earliest {
			earliest = day
		}
	}
	if earliest == "" {
		return nil, nil
	}
	from, _ := time.ParseInLocation("2006-01-02", earliest, loc)
	if last := end.AddDate(0, 0, -(growthMaxDays - 1)); from.Before(last) {
		from = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, loc)
	}
	g := &reportGrowth{}
	// Species first heard before the window start the curve already counted.
	for day, names := range byDay {
		if day < from.Format("2006-01-02") {
			g.Total += len(names)
		}
	}
	for t := from; !t.After(end); t = t.AddDate(0, 0, 1) {
		day := t.Format("2006-01-02")
		names := byDay[day]
		sort.Strings(names)
		g.Total += len(names)
		gd := growthDay{Date: day, New: len(names), Total: g.Total}
		if len(names) > 6 {
			gd.Species = append(names[:6:6], fmt.Sprintf("%d more", len(names)-6))
		} else {
			gd.Species = names
		}
		g.Days = append(g.Days, gd)
	}
	if len(g.Days) < 3 || g.Total < 5 {
		return nil, nil
	}
	return g, nil
}

// ---------------------------------------------------------------------------
// Weather and song

// reportWeather lines the hourly weather BirdNET-Go stores up with the hourly
// detection counts, one entry per hour from Start 00:00.
type reportWeather struct {
	Start   string        `json:"start"`
	Hours   []weatherHour `json:"hours"`
	Insight string        `json:"insight,omitempty"`
}

type weatherHour struct {
	N     int      `json:"n"`
	Temp  *float64 `json:"t,omitempty"` // °C
	Wind  *float64 `json:"w,omitempty"` // m/s
	Cloud *int     `json:"c,omitempty"` // percent
	Cond  string   `json:"o,omitempty"` // Clear, Clouds, Rain...
	Rain  bool     `json:"r,omitempty"`
}

func collectWeather(ctx context.Context, api func(string, any) error, d *reportData, hourlyByDay [][]int) *reportWeather {
	if len(d.Daily) == 0 || len(hourlyByDay) != len(d.Daily) {
		return nil
	}
	w := &reportWeather{Start: d.Daily[0].Date, Hours: make([]weatherHour, 24*len(d.Daily))}
	seen := 0
	for i, day := range d.Daily {
		for h := 0; h < 24; h++ {
			w.Hours[i*24+h].N = hourlyByDay[i][h]
		}
		var resp struct {
			Data []struct {
				Time    string   `json:"time"`
				Temp    *float64 `json:"temperature"`
				Wind    *float64 `json:"wind_speed"`
				Clouds  *int     `json:"clouds"`
				Main    string   `json:"weather_main"`
				Desc    string   `json:"weather_desc"`
				Missing bool     `json:"-"`
			} `json:"data"`
		}
		if err := api("/weather/hourly/"+day.Date, &resp); err != nil {
			continue
		}
		for _, r := range resp.Data {
			h, err := strconv.Atoi(strings.SplitN(r.Time, ":", 2)[0])
			if err != nil || h < 0 || h > 23 || r.Temp == nil {
				continue
			}
			wh := &w.Hours[i*24+h]
			wh.Temp, wh.Wind, wh.Cloud, wh.Cond = r.Temp, r.Wind, r.Clouds, r.Main
			low := strings.ToLower(r.Main + " " + r.Desc)
			wh.Rain = strings.Contains(low, "rain") || strings.Contains(low, "drizzle") || strings.Contains(low, "thunder") || strings.Contains(low, "shower")
			seen++
		}
	}
	if seen < 12 {
		return nil
	}
	w.Insight = weatherInsight(w)
	return w
}

// weatherInsight is one or two plain sentences: the temperature range, and
// what rain did to the singing at the same hours of day.
func weatherInsight(w *reportWeather) string {
	lo, hi := math.Inf(1), math.Inf(-1)
	var rainN, dryN, rainHours, dryHours int
	for i, h := range w.Hours {
		if h.Temp != nil {
			lo, hi = math.Min(lo, *h.Temp), math.Max(hi, *h.Temp)
		}
		if hod := i % 24; h.Temp != nil && hod >= 7 && hod <= 18 { // daylight, so night doesn't skew it
			if h.Rain {
				rainN += h.N
				rainHours++
			} else {
				dryN += h.N
				dryHours++
			}
		}
	}
	var out []string
	if !math.IsInf(lo, 1) {
		out = append(out, fmt.Sprintf("Temperatures ranged from %.0f °C to %.0f °C.", lo, hi))
	}
	if rainHours >= 3 && dryHours >= 10 && dryN > 0 {
		ch := (float64(rainN)/float64(rainHours))/(float64(dryN)/float64(dryHours)) - 1
		switch {
		case ch <= -0.2:
			out = append(out, fmt.Sprintf("Rain quieted the station: %d%% fewer detections an hour while it rained.", int(math.Round(-ch*100))))
		case ch >= 0.2:
			out = append(out, fmt.Sprintf("The birds sang through the rain: %d%% more detections an hour than in the dry.", int(math.Round(ch*100))))
		default:
			out = append(out, "Rain made little difference to the singing.")
		}
	} else if rainHours == 0 {
		out = append(out, "It stayed dry.")
	}
	return strings.Join(out, " ")
}

// ---------------------------------------------------------------------------
// Soundscape

// reportSoundscape is the station's sound level by hour of day and octave band,
// from the metrics BirdNET-Go exports to Prometheus. Levels are BirdNET-Go's
// own scale (relative, not calibrated); only the pattern is meant.
type reportSoundscape struct {
	Bands    []string     `json:"bands"`  // low to high
	Levels   [][]*float64 `json:"levels"` // [band][hour] mean level over the period
	Min      float64      `json:"min"`
	Max      float64      `json:"max"`
	Quietest int          `json:"quietest"` // hour of day, all bands together
	Loudest  int          `json:"loudest"`
	Insight  string       `json:"insight,omitempty"`
}

var octaveCenters = []float64{31.5, 63, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// bandHz parses BirdNET-Go's band labels ("1.0_kHz", "250.0_Hz").
func bandHz(label string) (float64, bool) {
	num, unit, ok := strings.Cut(label, "_")
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(unit) {
	case "hz":
		return v, true
	case "khz":
		return v * 1000, true
	}
	return 0, false
}

func bandName(hz float64) string {
	if hz >= 1000 {
		return strconv.FormatFloat(hz/1000, 'f', -1, 64) + " kHz"
	}
	return strconv.FormatFloat(hz, 'f', -1, 64) + " Hz"
}

func collectSoundscape(ctx context.Context, rc reportConfig, d *reportData, loc *time.Location, now time.Time) (*reportSoundscape, error) {
	metric := orDefault(rc.SoundMetric, "myaudio_octave_band_level_db")
	first, _ := time.ParseInLocation("2006-01-02", d.Period.Start, loc)
	// A past week stops at its own last evening.
	if last, err := time.ParseInLocation("2006-01-02", d.Period.End, loc); err == nil && last.AddDate(0, 0, 1).Before(now) {
		now = last.AddDate(0, 0, 1)
	}
	q := url.Values{
		"query": {fmt.Sprintf("avg by (frequency_band) (avg_over_time(%s[1h]))", metric)},
		"start": {strconv.FormatInt(first.Unix(), 10)}, "end": {strconv.FormatInt(now.Unix(), 10)}, "step": {"3600"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(rc.Prometheus, "/")+"/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if rc.PrometheusUser != "" {
		req.SetBasicAuth(rc.PrometheusUser, rc.PrometheusPass)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("prometheus: %s", resp.Status)
	}
	var body struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][2]any          `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return soundscapeFrom(body.Data.Result, loc)
}

// soundscapeFrom averages the hourly samples by hour of day for the octave
// bands among the series.
func soundscapeFrom(result []struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"`
}, loc *time.Location) (*reportSoundscape, error) {
	type acc struct{ sum, n [24]float64 }
	bands := map[float64]*acc{}
	for _, s := range result {
		hz, ok := bandHz(s.Metric["frequency_band"])
		if !ok {
			continue
		}
		center := 0.0
		for _, c := range octaveCenters {
			if math.Abs(hz-c)/c < 0.02 {
				center = c
			}
		}
		if center == 0 {
			continue
		}
		a := bands[center]
		if a == nil {
			a = &acc{}
			bands[center] = a
		}
		for _, v := range s.Values {
			ts, _ := v[0].(float64)
			val, err := strconv.ParseFloat(fmt.Sprint(v[1]), 64)
			if err != nil || math.IsNaN(val) || math.IsInf(val, 0) {
				continue
			}
			h := time.Unix(int64(ts), 0).In(loc).Hour()
			a.sum[h] += val
			a.n[h]++
		}
	}
	if len(bands) < 3 {
		return nil, fmt.Errorf("only %d octave bands in the metrics", len(bands))
	}
	var centers []float64
	for c := range bands {
		centers = append(centers, c)
	}
	sort.Float64s(centers)
	ss := &reportSoundscape{Min: math.Inf(1), Max: math.Inf(-1)}
	var allSum, allN [24]float64
	for _, c := range centers {
		a := bands[c]
		row := make([]*float64, 24)
		for h := 0; h < 24; h++ {
			if a.n[h] == 0 {
				continue
			}
			v := math.Round(a.sum[h]/a.n[h]*10) / 10
			row[h] = &v
			ss.Min, ss.Max = math.Min(ss.Min, v), math.Max(ss.Max, v)
			allSum[h] += v
			allN[h]++
		}
		ss.Bands = append(ss.Bands, bandName(c))
		ss.Levels = append(ss.Levels, row)
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for h := 0; h < 24; h++ {
		if allN[h] == 0 {
			continue
		}
		m := allSum[h] / allN[h]
		if m < lo {
			lo, ss.Quietest = m, h
		}
		if m > hi {
			hi, ss.Loudest = m, h
		}
	}
	if math.IsInf(lo, 1) {
		return nil, fmt.Errorf("no samples")
	}
	// Where most songbirds sing (2–8 kHz): when it peaks, and how far it rises.
	var bird [24]float64
	var birdN [24]float64
	for i, c := range centers {
		if c < 2000 || c > 8000 {
			continue
		}
		for h := 0; h < 24; h++ {
			if v := ss.Levels[i][h]; v != nil {
				bird[h] += *v
				birdN[h]++
			}
		}
	}
	blo, bhi, bpeak := math.Inf(1), math.Inf(-1), 0
	for h := 0; h < 24; h++ {
		if birdN[h] == 0 {
			continue
		}
		m := bird[h] / birdN[h]
		blo = math.Min(blo, m)
		if m > bhi {
			bhi, bpeak = m, h
		}
	}
	ss.Insight = fmt.Sprintf("Overall the station was quietest at %s and loudest at %s.", hourLabel(ss.Quietest), hourLabel(ss.Loudest))
	if !math.IsInf(blo, 1) && bhi-blo >= 1 {
		ss.Insight += fmt.Sprintf(" Between 2 and 8 kHz, where most songbirds call, it peaked at %s, %.0f dB above its quietest hour.", hourLabel(bpeak), bhi-blo)
	}
	return ss, nil
}
