package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fetchDetections returns every detection between two station-local dates
// (inclusive), newest first, false positives included: callers decide what to
// do with them.
func fetchDetections(ctx context.Context, client *http.Client, base, start, end string) ([]apiDetection, error) {
	const page = 1000
	var out []apiDetection
	for offset := 0; offset < 200*page; offset += page {
		q := url.Values{
			"queryType": {"all"}, "start_date": {start}, "end_date": {end},
			"numResults": {strconv.Itoa(page)}, "offset": {strconv.Itoa(offset)}, "sortBy": {"date_desc"},
		}
		b, _, err := get(ctx, client, base+"/api/v2/detections?"+q.Encode())
		if err != nil {
			return nil, err
		}
		var p struct {
			Data  []apiDetection `json:"data"`
			Total int            `json:"total"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, fmt.Errorf("detections: %w", err)
		}
		out = append(out, p.Data...)
		if len(p.Data) < page || len(out) >= p.Total {
			break
		}
	}
	return out, nil
}

// isRemoved reports whether a detection was marked a false positive.
func (d apiDetection) isRemoved() bool { return d.Verified == "false_positive" }

// when parses the detection's timestamp (it carries the station's offset).
func (d apiDetection) when() time.Time {
	t, _ := time.Parse(time.RFC3339, d.Timestamp)
	return t
}

// source is the camera or microphone name.
func (d apiDetection) source() string {
	if d.Source.DisplayName != "" {
		return d.Source.DisplayName
	}
	return d.Source.ID
}

// multi reports whether two or more models contributed to the detection.
func (d apiDetection) multi() bool { return len(d.Models) >= 2 }

// isNight is the report's fixed 7 pm–6 am window (real sunrise would reveal
// the station's latitude, and the audit needn't be exact).
func (d apiDetection) isNight() bool {
	h := d.when().Hour()
	return h >= 19 || h < 6
}

// speciesStat summarises the kept detections of one species.
type speciesStat struct {
	Sci, Common string
	N           int
	Multi       int // two or more models agreed
	Night       int
	High        int // confidence of 0.8 or more
	ConfSum     float64
	Confs       []float64
	BySource    map[string]int
}

func (s *speciesStat) meanConf() float64 {
	if s.N == 0 {
		return 0
	}
	return s.ConfSum / float64(s.N)
}

func (s *speciesStat) medianConf() float64 {
	if len(s.Confs) == 0 {
		return 0
	}
	c := append([]float64(nil), s.Confs...)
	sort.Float64s(c)
	return c[len(c)/2]
}

func (s *speciesStat) multiShare() float64 { return share(s.Multi, s.N) }
func (s *speciesStat) nightShare() float64 { return share(s.Night, s.N) }

// topSource is the source that heard the species most, and how many of its
// detections that was.
func (s *speciesStat) topSource() (string, int) {
	best, n := "", 0
	for name, c := range s.BySource {
		if c > n || (c == n && name < best) {
			best, n = name, c
		}
	}
	return best, n
}

func share(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// detectionStats is what the audit and the report's quality panel both read.
type detectionStats struct {
	Kept, Removed int
	Species       map[string]*speciesStat
	BySource      map[string]int
	SourceSpecies map[string]map[string]int // source -> species -> count
}

// summarise tallies the detections, leaving out false positives (counted
// separately) and any species or source whose name matches skip.
func summarise(dets []apiDetection) *detectionStats {
	st := &detectionStats{Species: map[string]*speciesStat{}, BySource: map[string]int{}, SourceSpecies: map[string]map[string]int{}}
	for _, d := range dets {
		if d.isRemoved() {
			st.Removed++
			continue
		}
		st.Kept++
		s := st.Species[d.Sci]
		if s == nil {
			s = &speciesStat{Sci: d.Sci, Common: d.Common, BySource: map[string]int{}}
			st.Species[d.Sci] = s
		}
		s.N++
		s.ConfSum += d.Confidence
		s.Confs = append(s.Confs, d.Confidence)
		if d.multi() {
			s.Multi++
		}
		if d.isNight() {
			s.Night++
		}
		if d.Confidence >= 0.8 {
			s.High++
		}
		src := d.source()
		s.BySource[src]++
		st.BySource[src]++
		if st.SourceSpecies[src] == nil {
			st.SourceSpecies[src] = map[string]int{}
		}
		st.SourceSpecies[src][d.Sci]++
	}
	return st
}

// ranked returns the species, most detections first.
func (st *detectionStats) ranked() []*speciesStat {
	out := make([]*speciesStat, 0, len(st.Species))
	for _, s := range st.Species {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Sci < out[j].Sci
	})
	return out
}

// dateRange returns the station-local dates (YYYY-MM-DD) that cover the last
// hours ending at now.
func dateRange(now time.Time, hours int) (string, string) {
	start := now.Add(-time.Duration(hours) * time.Hour)
	return start.Format("2006-01-02"), now.Format("2006-01-02")
}

func pctStr(f float64) string { return strconv.Itoa(int(f*100+0.5)) + "%" }

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func joinAnd(items []string) string { return joinNames(items) }

func trimSlash(s string) string { return strings.TrimRight(s, "/") }
