package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Location modes for coordinates found in recorded JSON.
const (
	locationHide   = "hide"   // remove the fields
	locationCoarse = "coarse" // round to whole degrees (~100 km)
	locationKeep   = "keep"
)

var coordKeys = map[string]bool{"lat": true, "lon": true, "lng": true, "latitude": true, "longitude": true}

// redactor scrubs recorded JSON before it is published.
type redactor struct {
	location string
	patterns []*regexp.Regexp // replaced with "redacted"
	forbid   []*regexp.Regexp // any match fails the build
}

func newRedactor(c config) (*redactor, error) {
	r := &redactor{location: c.Location}
	switch r.location {
	case locationHide, locationCoarse, locationKeep:
	case "":
		r.location = locationHide
	default:
		return nil, fmt.Errorf("-location must be hide, coarse or keep, not %q", c.Location)
	}
	for _, p := range c.Redact {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("-redact %q: %w", p, err)
		}
		r.patterns = append(r.patterns, re)
	}
	for _, p := range c.Forbid {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("-forbid %q: %w", p, err)
		}
		r.forbid = append(r.forbid, re)
	}
	return r, nil
}

// apply returns the publishable body of a capture.
func (r *redactor) apply(c *capture) []byte {
	if !isJSON(c.MIME) {
		return c.Body
	}
	b := rtspRe.ReplaceAll(c.Body, []byte("stream/$1"))
	for _, re := range r.patterns {
		b = re.ReplaceAll(b, []byte("redacted"))
	}
	if r.location != locationKeep && hasCoordKey(b) {
		var v any
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		if dec.Decode(&v) == nil {
			if changed := r.scrubCoords(v); changed {
				if out, err := json.Marshal(v); err == nil {
					b = out
				}
			}
		}
	}
	return b
}

// check returns an error naming the first forbidden pattern found in body.
func (r *redactor) check(key string, body []byte) error {
	for _, re := range r.forbid {
		if loc := re.FindIndex(body); loc != nil {
			start := max(0, loc[0]-30)
			end := min(len(body), loc[1]+30)
			return fmt.Errorf("forbidden pattern %q in %s: …%s…", re, key, bytes.ReplaceAll(body[start:end], []byte("\n"), []byte(" ")))
		}
	}
	return nil
}

var coordKeyRe = regexp.MustCompile(`(?i)"(lat|lon|lng|latitude|longitude)"\s*:\s*-?[0-9]`)

func hasCoordKey(b []byte) bool { return coordKeyRe.Match(b) }

// scrubCoords removes or rounds numeric coordinate fields anywhere in v.
func (r *redactor) scrubCoords(v any) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if coordKeys[strings.ToLower(k)] {
				if n, ok := val.(json.Number); ok {
					if r.location == locationHide {
						delete(t, k)
					} else if f, err := n.Float64(); err == nil {
						t[k] = math.Round(f)
					}
					changed = true
					continue
				}
			}
			if r.scrubCoords(val) {
				changed = true
			}
		}
	case []any:
		for _, e := range t {
			if r.scrubCoords(e) {
				changed = true
			}
		}
	}
	return changed
}
