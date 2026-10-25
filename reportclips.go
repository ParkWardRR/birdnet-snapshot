package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// reportRecordings are the clips the report lets visitors play: the most
// confident recordings of the period and the latest ones, one per species in
// each list.
type reportRecordings struct {
	Best   []reportClip `json:"best"`
	Recent []reportClip `json:"recent"`
}

type reportClip struct {
	Common      string  `json:"common"`
	Scientific  string  `json:"scientific"`
	Code        string  `json:"code"`
	Confidence  float64 `json:"confidence"`
	Time        string  `json:"time"`
	Models      int     `json:"models"`
	Audio       string  `json:"audio"`
	Spectrogram string  `json:"spectrogram,omitempty"`
}

// apiDetection is the part of a BirdNET-Go detection the clip picker reads.
type apiDetection struct {
	ID         int64   `json:"id"`
	Timestamp  string  `json:"timestamp"`
	Code       string  `json:"speciesCode"`
	Sci        string  `json:"scientificName"`
	Common     string  `json:"commonName"`
	Confidence float64 `json:"confidence"`
	ClipName   string  `json:"clipName"`
	Verified   string  `json:"verified"`
	Source     struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"source"`
	Models []struct {
		Name string `json:"modelName"`
	} `json:"modelContributions"`
}

// clipCandidates is how many detections each list looks through.
const clipCandidates = 1000

// fetchRecordings picks rc.Clips best and rc.Clips most recent recordings and
// stores their audio and spectrogram under /_snap/. The same privacy rules as
// the snapshot apply: nothing with c.NoAudio, nothing from a source in
// c.SkipAudio or an unknown source, nothing marked a false positive, and only
// species the report lists, at -clip-min confidence or better.
func fetchRecordings(ctx context.Context, c config, rc reportConfig, d *reportData, save func([]byte, string) (string, error)) error {
	if rc.Clips <= 0 || c.NoAudio {
		return nil
	}
	client := &http.Client{Timeout: 60 * time.Second}
	listed := map[string]bool{}
	for _, sp := range d.Species {
		listed[sp.Scientific] = true
	}
	list := func(sortBy string) ([]apiDetection, error) {
		var out []apiDetection
		for offset := 0; offset < clipCandidates; offset += 500 {
			q := url.Values{
				"queryType": {"all"}, "start_date": {d.Period.Start}, "end_date": {d.Period.End},
				"numResults": {"500"}, "offset": {fmt.Sprint(offset)}, "sortBy": {sortBy},
			}
			b, _, err := get(ctx, client, c.BirdNET+"/api/v2/detections?"+q.Encode())
			if err != nil {
				return nil, err
			}
			var page struct {
				Data  []apiDetection `json:"data"`
				Total int            `json:"total"`
			}
			if err := json.Unmarshal(b, &page); err != nil {
				return nil, err
			}
			out = append(out, page.Data...)
			if len(page.Data) < 500 || offset+500 >= page.Total {
				break
			}
		}
		return out, nil
	}
	eligible := func(x apiDetection) bool {
		return x.ClipName != "" && x.Verified != "false_positive" && listed[x.Sci] && x.Confidence >= rc.ClipMin &&
			!skipSource(strings.ToLower(x.Source.ID+" "+x.Source.DisplayName), c.SkipAudio)
	}
	used := map[int64]bool{}
	pick := func(cands []apiDetection) []reportClip {
		var out []reportClip
		species := map[string]bool{}
		for _, x := range cands {
			if len(out) == rc.Clips {
				break
			}
			if used[x.ID] || species[x.Sci] || !eligible(x) {
				continue
			}
			clip, err := storeClip(ctx, client, c, x, save)
			if err != nil {
				continue // not written yet, or gone: try the next one
			}
			used[x.ID], species[x.Sci] = true, true
			out = append(out, clip)
		}
		return out
	}

	best, err := list("confidence_desc")
	if err != nil {
		return fmt.Errorf("detections by confidence: %w", err)
	}
	// Clips that two or more models agreed on come first (many single-model
	// hits reach 1.0 too), then confidence, then the number of models.
	sort.SliceStable(best, func(i, j int) bool {
		ai, aj := len(best[i].Models) >= 2, len(best[j].Models) >= 2
		if ai != aj {
			return ai
		}
		if best[i].Confidence != best[j].Confidence {
			return best[i].Confidence > best[j].Confidence
		}
		return len(best[i].Models) > len(best[j].Models)
	})
	recent, err := list("date_desc")
	if err != nil {
		return fmt.Errorf("recent detections: %w", err)
	}
	r := &reportRecordings{Best: pick(best), Recent: pick(recent)}
	if len(r.Best)+len(r.Recent) > 0 {
		d.Recordings = r
	}
	log.Printf("report: %d best and %d recent recordings", len(r.Best), len(r.Recent))
	return nil
}

// storeClip saves one detection's audio (required) and spectrogram (if
// BirdNET-Go has one) and describes them.
func storeClip(ctx context.Context, client *http.Client, c config, x apiDetection, save func([]byte, string) (string, error)) (reportClip, error) {
	audio, mime, err := get(ctx, client, fmt.Sprintf("%s/api/v2/audio/%d", c.BirdNET, x.ID))
	if err != nil {
		return reportClip{}, err
	}
	ext := audioExt(mime)
	if ext == "" || len(audio) < 1024 {
		return reportClip{}, fmt.Errorf("clip %d: %s, %d bytes", x.ID, mime, len(audio))
	}
	clip := reportClip{
		Common: x.Common, Scientific: x.Sci, Code: x.Code, Confidence: round2(x.Confidence),
		Time: x.Timestamp, Models: len(x.Models),
	}
	if clip.Audio, err = save(audio, ext); err != nil {
		return reportClip{}, err
	}
	if img, mime, err := get(ctx, client, fmt.Sprintf("%s/api/v2/spectrogram/%d?size=md", c.BirdNET, x.ID)); err == nil && strings.HasPrefix(mime, "image/png") {
		if clip.Spectrogram, err = save(img, ".png"); err != nil {
			return reportClip{}, err
		}
	}
	return clip, nil
}

func audioExt(mime string) string {
	m, _, _ := strings.Cut(strings.ToLower(mime), ";")
	switch strings.TrimSpace(m) {
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return ".m4a"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	}
	return ""
}
