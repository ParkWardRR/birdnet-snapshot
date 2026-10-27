package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"net/url"
	"time"
)

// reportDawn is the dawn-chorus onset of the morning singers, one series per
// species and one point per day of the period.
type reportDawn struct {
	Series []reportOnset `json:"series"`
}

type reportOnset struct {
	Code   string           `json:"code"`
	Common string           `json:"common"`
	Days   []reportOnsetDay `json:"days"`
}

type reportOnsetDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
	// Minutes after civil dawn the species started (negative: before). nil
	// when the day can't say: too few detections, or the station wasn't
	// listening when the chorus began.
	Minutes *int `json:"minutes"`
}

// onsetPoint is one day of BirdNET-Go's /analytics/time/dawn-onset response.
type onsetPoint struct {
	Date  string   `json:"date"`
	Onset *float64 `json:"onsetRelMinutes"`
	Count int      `json:"detectionCount"`
}

const (
	dawnCast          = 5    // look for singers among this many most-heard species
	dawnSeriesMax     = 4    // and chart at most this many
	dawnMinCount      = 30   // detections a species needs over the period
	dawnMaxNight      = 0.25 // species calling more than this at night aren't dawn singers
	dawnMinPoints     = 2    // days with an onset a series needs to be drawn
	dawnListeningHour = 6    // a first detection later than this means the station started after dawn
	dawnSettledHour   = 10   // today's chorus is over by now; before it, today's onset is unfinished
)

// fetchDawn adds the dawn-chorus onset of the most-heard morning singers.
// Onsets are relative to civil dawn, which BirdNET-Go works out from the
// station's coordinates; only the offset in minutes is copied, never the dawn
// time or the coordinates. A BirdNET-Go without the endpoint just gets no
// chart.
func fetchDawn(ctx context.Context, c config, d *reportData, now time.Time) {
	client := &http.Client{Timeout: 60 * time.Second}
	start := earliestHeard(d.Species, now.Location())
	var out []reportOnset
	for i, sp := range d.Species {
		if i >= dawnCast || len(out) == dawnSeriesMax {
			break
		}
		if sp.Count < dawnMinCount || sp.NightShare > dawnMaxNight {
			continue
		}
		q := url.Values{"start_date": {d.Period.Start}, "end_date": {d.Period.End}, "species": {sp.Common}}
		b, _, err := get(ctx, client, c.BirdNET+"/api/v2/analytics/time/dawn-onset?"+q.Encode())
		if err != nil {
			log.Printf("report: dawn onset unavailable (%v); leaving the chart out", err)
			return
		}
		var raw []onsetPoint
		if json.Unmarshal(b, &raw) != nil {
			log.Printf("report: dawn onset: unexpected response; leaving the chart out")
			return
		}
		days := onsetDays(d.Daily, raw, start, now)
		if countOnsets(days) >= dawnMinPoints {
			out = append(out, reportOnset{Code: sp.Code, Common: sp.Common, Days: days})
		}
	}
	if len(out) > 0 {
		d.Dawn = &reportDawn{Series: out}
	}
}

// onsetDays lines BirdNET-Go's per-day onsets up with the report's days and
// drops the ones that would mislead: the day the station started late (it
// heard its first bird after dawn, so the onset is when it began listening,
// not when the birds began) and today before the chorus has settled.
func onsetDays(days []reportDay, raw []onsetPoint, stationStart, now time.Time) []reportOnsetDay {
	byDate := map[string]onsetPoint{}
	for _, p := range raw {
		byDate[p.Date] = p
	}
	late := ""
	if !stationStart.IsZero() && stationStart.Hour() >= dawnListeningHour {
		late = stationStart.Format("2006-01-02")
	}
	today := now.Format("2006-01-02")
	out := make([]reportOnsetDay, 0, len(days))
	for _, day := range days {
		p := byDate[day.Date]
		od := reportOnsetDay{Date: day.Date, Count: p.Count}
		unsettled := day.Date == today && now.Hour() < dawnSettledHour
		if p.Onset != nil && day.Date != late && !unsettled {
			m := int(math.Round(*p.Onset))
			od.Minutes = &m
		}
		out = append(out, od)
	}
	return out
}

func countOnsets(days []reportOnsetDay) int {
	n := 0
	for _, d := range days {
		if d.Minutes != nil {
			n++
		}
	}
	return n
}

// earliestHeard is the first detection of the period across all species, in
// the station's zone (zero if none parses).
func earliestHeard(species []reportSpecies, loc *time.Location) time.Time {
	var first time.Time
	for _, sp := range species {
		t, err := time.Parse(time.RFC3339, sp.FirstHeard)
		if err == nil && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	return first.In(loc)
}
