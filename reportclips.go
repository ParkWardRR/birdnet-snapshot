package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
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

// clipEligible says whether a detection's clip may be published: it has a
// clip, was not thrown out as a false positive, belongs to a species the
// report lists, reaches -clip-min, and does not come from a withheld source
// (c.SkipAudio, or an unknown one). c.NoAudio publishes none.
func clipEligible(c config, rc reportConfig, d *reportData) func(apiDetection) bool {
	listed := map[string]bool{}
	for _, sp := range d.Species {
		listed[sp.Scientific] = true
	}
	return func(x apiDetection) bool {
		return !c.NoAudio && x.ClipName != "" && !x.isRemoved() && listed[x.Sci] && x.Confidence >= rc.ClipMin &&
			!skipSource(strings.ToLower(x.Source.ID+" "+x.Source.DisplayName), c.SkipAudio)
	}
}

// bestFirst orders detections for "best recording" picks: clips two or more
// models agreed on first (many single-model hits reach 1.0 too), then
// confidence, then the number of models.
func bestFirst(ds []apiDetection) []apiDetection {
	out := append([]apiDetection(nil), ds...)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].multi(), out[j].multi()
		if ai != aj {
			return ai
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return len(out[i].Models) > len(out[j].Models)
	})
	return out
}

// fetchRecordings picks rc.Clips best and rc.Clips most recent recordings from
// the period's detections and stores their audio and spectrogram under /_snap/,
// one per species in each list.
func fetchRecordings(ctx context.Context, c config, rc reportConfig, d *reportData, dets []apiDetection, save func([]byte, string) (string, error)) error {
	if rc.Clips <= 0 || c.NoAudio {
		return nil
	}
	client := &http.Client{Timeout: 60 * time.Second}
	eligible := clipEligible(c, rc, d)
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
	recent := append([]apiDetection(nil), dets...)
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].when().After(recent[j].when()) })
	r := &reportRecordings{Best: pick(bestFirst(dets)), Recent: pick(recent)}
	if len(r.Best)+len(r.Recent) > 0 {
		d.Recordings = r
	}
	log.Printf("report: %d best and %d recent recordings", len(r.Best), len(r.Recent))
	return nil
}

// speciesViewTop is how many of the most-heard species get their own
// recordings for the species view.
const speciesViewTop = 12

// fetchSpeciesClips stores up to rc.SpeciesClips of the best recordings of each
// of the most-heard species, under the same rules as the Listen section.
func fetchSpeciesClips(ctx context.Context, c config, rc reportConfig, d *reportData, dets []apiDetection, save func([]byte, string) (string, error)) {
	if rc.SpeciesClips <= 0 || c.NoAudio {
		return
	}
	client := &http.Client{Timeout: 60 * time.Second}
	eligible := clipEligible(c, rc, d)
	bySpecies := map[string][]apiDetection{}
	for _, x := range dets {
		bySpecies[x.Sci] = append(bySpecies[x.Sci], x)
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := 0; i < len(d.Species) && i < speciesViewTop; i++ {
		sp := &d.Species[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			for _, x := range bestFirst(bySpecies[sp.Scientific]) {
				if len(sp.Clips) == rc.SpeciesClips {
					return
				}
				if !eligible(x) {
					continue
				}
				if clip, err := storeClip(ctx, client, c, x, save); err == nil {
					sp.Clips = append(sp.Clips, clip)
				}
			}
		}()
	}
	wg.Wait()
	n := 0
	for _, sp := range d.Species {
		n += len(sp.Clips)
	}
	log.Printf("report: %d recordings for the species views", n)
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
