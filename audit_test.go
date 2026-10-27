package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// det builds a detection the way BirdNET-Go sends it.
func det(id int64, common, sci, src string, conf float64, models int, ts time.Time) apiDetection {
	ms := make([]map[string]any, models)
	for i := range ms {
		ms[i] = map[string]any{"modelName": fmt.Sprint("m", i)}
	}
	b, _ := json.Marshal(map[string]any{
		"id": id, "timestamp": ts.Format(time.RFC3339), "commonName": common, "scientificName": sci, "speciesCode": sci[:3],
		"confidence": conf, "verified": "unverified", "source": map[string]any{"id": "rtsp://x/" + src, "displayName": src},
		"modelContributions": ms,
	})
	var d apiDetection
	if err := json.Unmarshal(b, &d); err != nil {
		panic(err)
	}
	return d
}

// noisyStation is a small copy of this week's trouble: a microphone that
// hears a Raven all day and night, a hummingbird at night, and healthy birds.
func noisyStation(now time.Time) []apiDetection {
	var ds []apiDetection
	id := int64(0)
	add := func(n int, common, sci, src string, conf float64, models int, hourFn func(i int) int) {
		for i := 0; i < n; i++ {
			id++
			ts := time.Date(now.Year(), now.Month(), now.Day(), hourFn(i), i%60, 0, 0, now.Location()).Add(-24 * time.Hour)
			ds = append(ds, det(id, common, sci, src, conf, models, ts))
		}
	}
	allDay := func(i int) int { return i % 24 }
	morning := func(i int) int { return 7 + i%6 }
	add(300, "Common Raven", "Corvus corax", "Garage Front", 0.4, 1, allDay)
	add(20, "Common Raven", "Corvus corax", "Alley", 0.45, 1, allDay)
	add(150, "House Finch", "Haemorhous mexicanus", "Alley", 0.8, 2, morning)
	add(30, "House Finch", "Haemorhous mexicanus", "Garage Front", 0.8, 2, morning)
	add(5, "Anna's Hummingbird", "Calypte anna", "Alley", 0.7, 1, func(i int) int { return 22 })
	add(1, "Anna's Hummingbird", "Calypte anna", "Alley", 0.99, 2, func(i int) int { return 8 })
	add(40, "Great Horned Owl", "Bubo virginianus", "Alley", 0.9, 2, func(i int) int { return 2 })
	return ds
}

func TestAuditFindsTheNoisyMicrophone(t *testing.T) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	found := audit(summarise(noisyStation(now)), defaultAudit())
	text := map[string]string{}
	for _, f := range found {
		text[f.Key] = f.Text
	}
	raven := text["species|Corvus corax"]
	for _, want := range []string{"Common Raven:", "of all detections", "94% of them from Garage Front", "median confidence 0.40", "0% confirmed by two models"} {
		if !strings.Contains(raven, want) {
			t.Errorf("raven finding lacks %q: %s", want, raven)
		}
	}
	if hb := text["species|Calypte anna"]; !strings.Contains(hb, "5 of 6 detections at night") {
		t.Errorf("hummingbird: %q", hb)
	}
	if src := text["source|Garage Front"]; !strings.Contains(src, "Common Raven (91%) and House Finch (9%); thin evidence for Common Raven (median confidence 0.40") {
		t.Errorf("source finding: %q", src)
	}
	for _, healthy := range []string{"species|Haemorhous mexicanus", "species|Bubo virginianus"} {
		if text[healthy] != "" {
			t.Errorf("flagged a healthy species: %s", text[healthy])
		}
	}
	if found[0].Severity != 2 {
		t.Errorf("warnings sort first: %+v", found[0])
	}

	cfg := defaultAudit()
	cfg.Ignore = regexp.MustCompile(`Raven`)
	for _, f := range audit(summarise(noisyStation(now)), cfg) {
		if strings.Contains(f.Text, "Common Raven:") {
			t.Errorf("ignored species still flagged: %s", f.Text)
		}
	}
	// Marked false positives leave the picture.
	ds := noisyStation(now)
	for i := range ds {
		if ds[i].Common == "Common Raven" {
			ds[i].Verified = "false_positive"
		}
	}
	st := summarise(ds)
	if st.Removed != 320 || len(audit(st, defaultAudit())) > 1 { // only the hummingbird note remains
		t.Errorf("after review: removed %d, findings %+v", st.Removed, audit(st, defaultAudit()))
	}
	if len(audit(&detectionStats{}, defaultAudit())) != 0 {
		t.Error("empty station")
	}
}

func TestDueRenotifies(t *testing.T) {
	now := time.Now()
	fs := []finding{{Key: "a"}, {Key: "b"}}
	st := auditState{Notified: map[string]time.Time{"a": now.Add(-time.Hour), "gone": now.Add(-time.Hour)}}
	fresh, next := due(fs, st, now, 7*24*time.Hour)
	if len(fresh) != 1 || fresh[0].Key != "b" {
		t.Errorf("fresh %+v", fresh)
	}
	if _, ok := next.Notified["gone"]; ok {
		t.Error("a resolved finding must be forgotten so it alerts again if it returns")
	}
	if fresh, _ = due(fs, next, now.Add(8*24*time.Hour), 7*24*time.Hour); len(fresh) != 2 {
		t.Errorf("unresolved findings repeat weekly: %+v", fresh)
	}
}

// ntfyServer records what the commands push.
type ntfyServer struct {
	*httptest.Server
	mu   sync.Mutex
	msgs []map[string]any
	auth string
	path string
}

func newNtfyServer(t *testing.T) *ntfyServer {
	s := &ntfyServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		s.mu.Lock()
		s.msgs, s.auth, s.path = append(s.msgs, m), r.Header.Get("Authorization"), r.URL.Path
		s.mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(s.Close)
	return s
}

func TestNtfySend(t *testing.T) {
	s := newNtfyServer(t)
	n := ntfy{URL: s.URL + "/birds", Token: "tk_123"}
	err := n.send(context.Background(), ntfyMessage{Title: "Rare visitor: Pine Grosbeak ♥", Message: "3 detections", Priority: 4, Tags: []string{"bird"}, Click: "http://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	m := s.msgs[0]
	if m["topic"] != "birds" || m["title"] != "Rare visitor: Pine Grosbeak ♥" || m["priority"] != float64(4) || m["click"] != "http://x/y" || s.auth != "Bearer tk_123" || s.path != "/" {
		t.Errorf("sent %+v auth=%q path=%q", m, s.auth, s.path)
	}
	if err := (ntfy{URL: "http://127.0.0.1:1/"}).send(context.Background(), ntfyMessage{}); err == nil {
		t.Error("a URL with no topic must be refused")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", 403) }))
	defer bad.Close()
	if err := (ntfy{URL: bad.URL + "/t"}).send(context.Background(), ntfyMessage{}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("server errors must surface: %v", err)
	}
}

// fakeStation serves the BirdNET-Go endpoints the audit and rare commands read.
func fakeStation(t *testing.T, dets []apiDetection) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/detections":
			off := r.URL.Query().Get("offset")
			if off != "0" {
				writeJSON(w, map[string]any{"data": []any{}, "total": len(dets)})
				return
			}
			writeJSON(w, map[string]any{"data": dets, "total": len(dets)})
		case "/api/v2/detections/recent":
			writeJSON(w, []map[string]any{{"timestamp": dets[0].Timestamp}})
		case "/api/v2/species":
			status, score := "very_common", 0.98
			if strings.HasPrefix(r.URL.Query().Get("scientific_name"), "Loxia") || strings.HasPrefix(r.URL.Query().Get("scientific_name"), "Bucephala") {
				status, score = "uncommon", 0.47
			}
			writeJSON(w, map[string]any{"rarity": map[string]any{"status": status, "score": score}})
		case "/api/v2/analytics/species/summary":
			writeJSON(w, []map[string]any{
				{"scientific_name": "Loxia curvirostra", "first_heard": dets[0].Timestamp},
				{"scientific_name": "Haemorhous mexicanus", "first_heard": "2026-10-01T07:00:00-07:00"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRareVisitors(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	now := time.Now().In(loc)
	at := func(min int) time.Time { return now.Add(-time.Duration(min) * time.Minute) }
	dets := []apiDetection{
		det(3, "Red Crossbill", "Loxia curvirostra", "Doorbell", 0.94, 2, at(10)),
		det(2, "Red Crossbill", "Loxia curvirostra", "Alley", 0.96, 2, at(25)),
		det(1, "Red Crossbill", "Loxia curvirostra", "Alley", 0.65, 1, at(60)),
		det(9, "Common Goldeneye", "Bucephala clangula", "Alley", 0.95, 2, at(15)), // a lone hit waits
		det(8, "House Finch", "Haemorhous mexicanus", "Alley", 0.9, 2, at(5)),
		det(7, "House Finch", "Haemorhous mexicanus", "Alley", 0.9, 2, at(6)),
	}
	station := fakeStation(t, dets)
	push := newNtfyServer(t)
	notify := ntfyFlags{ntfy: ntfy{URL: push.URL + "/birds"}, Link: "http://birdnet.example"}
	state := filepath.Join(t.TempDir(), "rare.json")

	if err := runRare(context.Background(), station.URL, defaultRare(), notify, state, false); err != nil {
		t.Fatal(err)
	}
	if len(push.msgs) != 1 {
		t.Fatalf("want one alert, got %+v", push.msgs)
	}
	m := push.msgs[0]
	msg := m["message"].(string)
	for _, want := range []string{"3 detections in 50 minutes on 2 microphones", "Best 96%", "confirmed by two models", "Uncommon here (range score 0.47)", "First time at the station"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
	if m["title"] != "Rare visitor: Red Crossbill" || m["click"] != "http://birdnet.example/ui/detections/2" {
		t.Errorf("title/click: %v %v", m["title"], m["click"])
	}
	// The same visit is not announced again.
	if err := runRare(context.Background(), station.URL, defaultRare(), notify, state, false); err != nil {
		t.Fatal(err)
	}
	if len(push.msgs) != 1 {
		t.Errorf("repeat alert: %d messages", len(push.msgs))
	}
	// A dry run sends and remembers nothing.
	dry := filepath.Join(t.TempDir(), "dry.json")
	if err := runRare(context.Background(), station.URL, defaultRare(), notify, dry, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dry); err == nil || len(push.msgs) != 1 {
		t.Error("dry run wrote state or sent")
	}
}

func TestRunAuditSendsOnceAndWritesMetrics(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	now := time.Now().In(loc)
	ds := noisyStation(now.Add(24 * time.Hour)) // yesterday's hours, inside the 48 h window
	station := fakeStation(t, ds)
	push := newNtfyServer(t)
	notify := ntfyFlags{ntfy: ntfy{URL: push.URL + "/birds"}, Link: "http://birdnet.example"}
	dir := t.TempDir()
	state, prom := filepath.Join(dir, "audit.json"), filepath.Join(dir, "audit.prom")
	cfg := defaultAudit()
	cfg.Hours = 96

	for i := 0; i < 2; i++ {
		if err := runAudit(context.Background(), station.URL, cfg, notify, state, 7*24*time.Hour, prom, false, false, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if len(push.msgs) != 1 {
		t.Fatalf("want one push over two runs, got %d", len(push.msgs))
	}
	m := push.msgs[0]
	if !strings.HasPrefix(m["title"].(string), "BirdNET audit: 3 things to look at") || m["priority"] != float64(4) || m["click"] != "http://birdnet.example/ui/dashboard" {
		t.Errorf("push %+v", m)
	}
	if !strings.Contains(m["message"].(string), "• Common Raven:") {
		t.Errorf("body: %v", m["message"])
	}
	b, _ := os.ReadFile(prom)
	for _, want := range []string{`birdnet_audit_findings{check="species"} 2`, `birdnet_audit_findings{check="source"} 1`, "birdnet_audit_last_run_success 1", "birdnet_audit_warnings 2"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("metrics lack %q:\n%s", want, b)
		}
	}
	// A failing fetch records the failure but keeps the last success time.
	if err := writeAuditMetrics(prom, nil, 0, time.Now(), fmt.Errorf("boom")); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(prom)
	if !strings.Contains(string(b), "birdnet_audit_last_run_success 0") || readGauge(prom, "birdnet_audit_last_success_timestamp_seconds") == 0 {
		t.Errorf("failure metrics:\n%s", b)
	}
}
