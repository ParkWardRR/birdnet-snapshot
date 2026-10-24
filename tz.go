package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // TZ works in minimal containers without system zoneinfo
)

// serverZone returns the zone BirdNET-Go keeps its dates in, and the IANA name
// to give Chrome (empty: leave Chrome alone). "Today" on the dashboard is the
// server's day, so a crawler in another zone (a UTC container, say) would
// record the wrong dates. TZ wins when set; otherwise the offset is read from
// the newest detection's timestamp.
func serverZone(ctx context.Context, base string) (*time.Location, string) {
	if tz := os.Getenv("TZ"); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc, tz
		}
		log.Printf("tz: TZ=%q is not a known zone; ignoring it", tz)
	}
	off, ok := detectOffset(ctx, base)
	_, localOff := time.Now().Zone()
	if !ok || off == localOff {
		return time.Local, ""
	}
	loc := time.FixedZone(fmt.Sprintf("UTC%+03d:%02d", off/3600, abs(off%3600)/60), off)
	name := ""
	if off%3600 == 0 {
		name = fmt.Sprintf("Etc/GMT%+d", -off/3600) // Etc/GMT signs are inverted
		if off == 0 {
			name = "Etc/UTC"
		}
	}
	log.Printf("tz: BirdNET-Go is at %s but this machine at UTC%+d; using the server's zone (set TZ to silence this)", loc, localOff/3600)
	return loc, name
}

// detectOffset reads the UTC offset of the newest detection's timestamp.
func detectOffset(ctx context.Context, base string) (int, bool) {
	client := &http.Client{Timeout: 10 * time.Second}
	body, _, err := get(ctx, client, base+"/api/v2/detections/recent?limit=1")
	if err != nil {
		return 0, false
	}
	var dets []struct {
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal(body, &dets) != nil || len(dets) == 0 {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339, dets[0].Timestamp)
	if err != nil {
		return 0, false
	}
	_, off := t.Zone()
	return off, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
