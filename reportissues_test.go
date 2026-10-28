package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssueWeeks(t *testing.T) {
	loc, _ := time.LoadLocation("America/Los_Angeles")
	at := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, loc) }

	// ISO weeks straddle the new year: Friday 1 Jan 2027 is still week 53 of 2026.
	for _, c := range []struct {
		t    time.Time
		id   string
		mon  time.Time
		desc string
	}{
		{at(2026, 10, 27, 12), "2026-w44", at(2026, 10, 26, 0), "midweek"},
		{at(2026, 10, 25, 23), "2026-w43", at(2026, 10, 19, 0), "Sunday night"},
		{at(2027, 1, 1, 9), "2026-w53", at(2026, 12, 28, 0), "new year's day"},
		{at(2027, 1, 4, 0), "2027-w01", at(2027, 1, 4, 0), "first Monday"},
	} {
		if got := issueID(c.t); got != c.id {
			t.Errorf("%s: id %s, want %s", c.desc, got, c.id)
		}
		if got := weekStart(c.t); !got.Equal(c.mon) {
			t.Errorf("%s: week starts %v, want %v", c.desc, got, c.mon)
		}
	}

	first := at(2026, 10, 14, 7) // a Wednesday
	// Week 43 ends Sunday night; it closes an hour after midnight.
	if got := completedWeeks(first, at(2026, 10, 19, 0), 0); len(got) != 1 {
		// 42 is complete at the Monday midnight... but not yet settled
		if len(got) != 0 {
			t.Errorf("Monday 00:00 closes %d weeks", len(got))
		}
	}
	if got := completedWeeks(first, at(2026, 10, 19, 0).Add(30*time.Minute), 0); len(got) != 0 {
		t.Errorf("30 min after the week: %d weeks settled", len(got))
	}
	got := completedWeeks(first, at(2026, 11, 10, 12), 0)
	if len(got) != 4 || issueID(got[0]) != "2026-w42" || issueID(got[3]) != "2026-w45" {
		t.Errorf("weeks %v", got)
	}
	if got := completedWeeks(first, at(2026, 11, 10, 12), 2); len(got) != 2 || issueID(got[0]) != "2026-w44" {
		t.Errorf("limit keeps the latest: %v", got)
	}
	// Daylight saving ends on 1 Nov 2026: week 44 is still Monday to Sunday.
	got = completedWeeks(at(2026, 10, 26, 8), at(2026, 11, 2, 12), 0)
	if len(got) != 1 || got[0].Weekday() != time.Monday || got[0].Hour() != 0 {
		t.Errorf("across DST: %v", got)
	}
}

// issueHistory is a station that started on 2026-10-14, silent in week 43 (it
// was unplugged), with a different top bird each week.
func issueHistory() history {
	names := map[string]string{"Haemorhous mexicanus": "House Finch", "Sayornis nigricans": "Black Phoebe"}
	days := map[string]map[string]int{}
	for d := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC); d.Before(time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		switch id := issueID(d); id {
		case "2026-w43":
			days[key] = nil
		case "2026-w42":
			days[key] = map[string]int{"Haemorhous mexicanus": 20}
		default:
			days[key] = map[string]int{"Sayornis nigricans": 15, "Haemorhous mexicanus": 4}
		}
	}
	return history{names: names, days: days}
}

func TestIssueStore(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	srv := issueHistory().server(t)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, loc) // Wednesday of week 45

	store := issueStore{dir: filepath.Join(t.TempDir(), "issues")}
	c := config{BirdNET: srv.URL}
	rc := reportConfig{Title: "Field Notes", Subtitle: "s", Station: "A station", Days: 7, IssueWeeks: 12}

	created := store.ensure(context.Background(), c, rc, loc, now)
	// w42 and w44 are frozen; w43 had nothing; w45 is not over.
	if strings.Join(created, ",") != "2026-w42,2026-w44" {
		t.Fatalf("created %v", created)
	}
	if got := store.ids(); strings.Join(got, ",") != "2026-w42,2026-w44" {
		t.Fatalf("ids %v", got)
	}
	// Idempotent: a frozen issue is never rebuilt.
	before, _ := os.ReadFile(filepath.Join(store.dir, "2026-w42", "issue.json"))
	if again := store.ensure(context.Background(), c, rc, loc, now.Add(3*time.Hour)); len(again) != 0 {
		t.Fatalf("second run created %v", again)
	}
	after, _ := os.ReadFile(filepath.Join(store.dir, "2026-w42", "issue.json"))
	if string(before) != string(after) {
		t.Error("a frozen issue changed")
	}
	if _, err := os.Stat(filepath.Join(store.dir, "2026-w44.tmp")); err == nil {
		t.Error("scratch directory left behind")
	}

	// A frozen week holds that week's numbers, trimmed to when the station began listening (the 14th).
	var w42 reportData
	if err := json.Unmarshal(before, &w42); err != nil {
		t.Fatal(err)
	}
	if w42.Period.Start != "2026-10-14" || w42.Period.End != "2026-10-18" || w42.Totals.Species != 1 || w42.Species[0].Common != "House Finch" {
		t.Errorf("w42 %+v %v", w42.Period, w42.Totals)
	}
	if w42.Issue == nil || w42.Issue.ID != "2026-w42" {
		t.Errorf("issue marker %+v", w42.Issue)
	}

	// Assemble numbers and links the issues by order.
	site := t.TempDir()
	idx, err := store.assemble(site, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Issues) != 2 || idx.Issues[0].ID != "2026-w44" || idx.Issues[0].Number != 2 || idx.Issues[1].Number != 1 {
		t.Fatalf("index %+v", idx.Issues)
	}
	if idx.Issues[0].Top != "Black Phoebe" || idx.Issues[1].Top != "House Finch" {
		t.Errorf("tops %q %q", idx.Issues[0].Top, idx.Issues[1].Top)
	}
	var one map[string]any
	raw, err := os.ReadFile(filepath.Join(site, "data", "issues", "2026-w44.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &one)
	is := one["issue"].(map[string]any)
	if is["number"].(float64) != 2 || is["prev"] != "2026-w42" || is["next"] != nil || one["issueCount"].(float64) != 2 || one["feed"] != true {
		t.Errorf("issue meta %v issueCount %v feed %v", is, one["issueCount"], one["feed"])
	}
	if _, err := os.Stat(filepath.Join(site, "data", "issues.json")); err != nil {
		t.Error("no issues.json")
	}

	// A week added later (here w45, now over) does not renumber the others.
	later := now.AddDate(0, 0, 7)
	if got := store.ensure(context.Background(), c, rc, loc, later); strings.Join(got, ",") != "2026-w45" {
		t.Fatalf("later created %v", got)
	}
	idx, err = store.assemble(t.TempDir(), false)
	if err != nil || len(idx.Issues) != 3 || idx.Issues[0].Number != 3 || idx.Issues[2].ID != "2026-w42" {
		t.Fatalf("later index %+v %v", idx, err)
	}
}

func TestIssueFeed(t *testing.T) {
	idx := &issueIndex{Issues: []issueEntry{
		{ID: "2026-w44", Number: 2, Label: "Oct 26 – Nov 1", Published: "2026-11-02T00:10:00-08:00", Detections: 1234, Species: 12, Summary: "A busy week & a <loud> one.", PDF: "/_snap/field-notes-abc.pdf"},
		{ID: "2026-w42", Number: 1, Label: "Oct 12 – 18", Published: "2026-10-19T00:10:00-07:00", Detections: 20, Species: 1},
	}}
	dir := t.TempDir()
	if err := writeFeed(dir, "https://notes.example.com/", "Field Notes", "A week of birdsong", idx, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "feed.xml"))
	var f atomFeed
	if err := xml.Unmarshal(b, &f); err != nil {
		t.Fatalf("not valid XML: %v\n%s", err, b)
	}
	if !strings.HasPrefix(string(b), "<?xml") || f.ID != "https://notes.example.com/" || f.Updated != "2026-11-02T00:10:00-08:00" || len(f.Entries) != 2 {
		t.Fatalf("feed %+v", f)
	}
	e := f.Entries[0]
	if e.Title != "Field Notes No. 2: Oct 26 – Nov 1" || e.ID != "tag:notes.example.com,2026-11-02:field-notes:2026-w44" {
		t.Errorf("entry %+v", e)
	}
	if e.Links[0].Href != "https://notes.example.com/?issue=2026-w44" || e.Links[1].Rel != "enclosure" || e.Links[1].Href != "https://notes.example.com/_snap/field-notes-abc.pdf" {
		t.Errorf("links %+v", e.Links)
	}
	if !strings.Contains(e.Summary, "1,234 detections of 12 species") || !strings.Contains(e.Summary, "A busy week & a <loud> one.") {
		t.Errorf("summary %q", e.Summary)
	}
	if strings.Contains(string(b), "<loud>") {
		t.Error("summary was not escaped")
	}
}

func TestInjectHead(t *testing.T) {
	dir := t.TempDir()
	html := "<!doctype html><html><head><title>x</title></head><body></body></html>"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := injectHead(dir, "https://notes.example.com/", `Field "Notes" · Oct`, "1 <2> detections", "/_snap/share-1.png", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	s := string(b)
	for _, want := range []string{
		`og:image" content="https://notes.example.com/_snap/share-1.png"`,
		`og:url" content="https://notes.example.com/"`,
		`twitter:card" content="summary_large_image"`,
		`type="application/atom+xml"`,
		`og:description" content="1 &lt;2&gt; detections"`,
		`og:title" content="Field &#34;Notes&#34; · Oct"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	if strings.Index(s, "og:title") > strings.Index(s, "</head>") {
		t.Error("tags are outside <head>")
	}
	// Without a share image or an archive, only the basics.
	os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644)
	_ = injectHead(dir, "https://notes.example.com", "t", "d", "", false)
	b, _ = os.ReadFile(filepath.Join(dir, "index.html"))
	if strings.Contains(string(b), "og:image") || strings.Contains(string(b), "atom+xml") {
		t.Errorf("unexpected tags:\n%s", b)
	}
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644)
	if err := injectHead(dir, "https://x", "t", "d", "", false); err == nil {
		t.Error("a page without <head> should be an error")
	}
}

func TestIssueDigest(t *testing.T) {
	e := issueEntry{ID: "2026-w44", Number: 2, Label: "Oct 26 – Nov 1", Detections: 2345, Species: 14,
		highlights: []string{"one", "two", "three", "four", "five"}}
	m := issueDigest(e, "https://notes.example.com/")
	if m.Title != "Field Notes No. 2 is out" || m.Click != "https://notes.example.com/?issue=2026-w44" {
		t.Errorf("%+v", m)
	}
	if !strings.Contains(m.Message, "2,345 detections of 14 species") || !strings.Contains(m.Message, "• four") || strings.Contains(m.Message, "five") {
		t.Errorf("message %q", m.Message)
	}
	if m := issueDigest(e, ""); m.Click != "" {
		t.Errorf("no site, no link: %q", m.Click)
	}
}

func TestBuildReportWithIssues(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	// A station that has heard something every day for 20 days.
	days := map[string]map[string]int{}
	now := time.Now().In(loc)
	for back := 0; back <= 20; back++ {
		days[dayKey(loc, back)] = map[string]int{"Haemorhous mexicanus": 5}
	}
	srv := history{names: map[string]string{"Haemorhous mexicanus": "House Finch"}, days: days}.server(t)

	var pushed []map[string]any
	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		pushed = append(pushed, m)
	}))
	defer ntfySrv.Close()

	root := t.TempDir()
	c := config{BirdNET: srv.URL, Out: filepath.Join(root, "site")}
	rc := reportConfig{Title: "Field Notes", Subtitle: "s", Station: "A station", Days: 7, IssuesDir: filepath.Join(root, "issues"), IssueWeeks: 12,
		URL: "https://notes.example.com", Notify: ntfyFlags{ntfy: ntfy{URL: ntfySrv.URL + "/birds"}}}

	out, err := buildReportOutcome(context.Background(), c, rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Created) < 2 {
		t.Fatalf("expected earlier weeks to be frozen, got %d", len(out.Created))
	}
	for _, f := range []string{"index.html", "feed.xml", "data/report.json", "data/issues.json"} {
		if _, err := os.Stat(filepath.Join(c.Out, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	for _, e := range out.Created {
		if _, err := os.Stat(filepath.Join(c.Out, "data", "issues", e.ID+".json")); err != nil {
			t.Errorf("no page data for %s", e.ID)
		}
	}
	var roll reportData
	b, _ := os.ReadFile(filepath.Join(c.Out, "data", "report.json"))
	_ = json.Unmarshal(b, &roll)
	if roll.Issue != nil || roll.IssueCount != len(out.Created) || !roll.Feed {
		t.Errorf("rolling report: issue %v count %d feed %v", roll.Issue, roll.IssueCount, roll.Feed)
	}
	page, _ := os.ReadFile(filepath.Join(c.Out, "index.html"))
	if !strings.Contains(string(page), `og:url" content="https://notes.example.com/"`) || !strings.Contains(string(page), "atom+xml") {
		t.Error("index.html has no link-preview tags")
	}
	_ = now

	// The digest announces only the newest issue, and says there were earlier ones.
	notifyIssues(context.Background(), rc, out)
	if len(pushed) != 1 {
		t.Fatalf("%d pushes", len(pushed))
	}
	newest := out.Created[len(out.Created)-1]
	if !strings.Contains(pushed[0]["title"].(string), "No. "+itoa(newest.Number)) || !strings.Contains(pushed[0]["message"].(string), "earlier issue") {
		t.Errorf("push %v", pushed[0])
	}

	for _, e := range out.Created {
		if !out.store.announced(e.ID) {
			t.Errorf("issue %s sent but not marked", e.ID)
		}
	}

	// A second build creates and announces nothing.
	out2, err := buildReportOutcome(context.Background(), c, rc)
	if err != nil || len(out2.Created) != 0 {
		t.Fatalf("second build: %v %v", out2, err)
	}
	notifyIssues(context.Background(), rc, out2)
	if len(pushed) != 1 {
		t.Errorf("repeat push: %d", len(pushed))
	}
}

func TestDigestRetriesAfterFailure(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	days := map[string]map[string]int{}
	for back := 0; back <= 10; back++ {
		days[dayKey(loc, back)] = map[string]int{"Haemorhous mexicanus": 5}
	}
	srv := history{names: map[string]string{"Haemorhous mexicanus": "House Finch"}, days: days}.server(t)
	up, sent := false, 0
	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		sent++
	}))
	defer ntfySrv.Close()
	root := t.TempDir()
	c := config{BirdNET: srv.URL, Out: filepath.Join(root, "site")}
	rc := reportConfig{Title: "t", Days: 7, IssuesDir: filepath.Join(root, "issues"), IssueWeeks: 12, URL: "https://n.example.com", Notify: ntfyFlags{ntfy: ntfy{URL: ntfySrv.URL + "/birds"}}}

	out, err := buildReportOutcome(context.Background(), c, rc)
	if err != nil || len(out.Created) == 0 {
		t.Fatalf("%v %v", out, err)
	}
	notifyIssues(context.Background(), rc, out) // ntfy is down
	if sent != 0 || out.store.announced(out.Created[0].ID) {
		t.Fatal("a failed push was recorded as sent")
	}
	up = true
	out, _ = buildReportOutcome(context.Background(), c, rc) // nothing new, but the digest is still owed
	notifyIssues(context.Background(), rc, out)
	notifyIssues(context.Background(), rc, out)
	if sent != 1 {
		t.Fatalf("%d pushes, want 1", sent)
	}
}

func TestIssuesOffByDefault(t *testing.T) {
	t.Setenv("TZ", "America/Los_Angeles")
	loc, _ := time.LoadLocation("America/Los_Angeles")
	days := map[string]map[string]int{dayKey(loc, 0): {"Haemorhous mexicanus": 5}}
	srv := history{names: map[string]string{"Haemorhous mexicanus": "House Finch"}, days: days}.server(t)
	c := config{BirdNET: srv.URL, Out: filepath.Join(t.TempDir(), "site")}
	out, err := buildReportOutcome(context.Background(), c, reportConfig{Title: "t", Days: 7})
	if err != nil || len(out.Created) != 0 {
		t.Fatalf("%v %v", out, err)
	}
	for _, f := range []string{"feed.xml", "data/issues.json"} {
		if _, err := os.Stat(filepath.Join(c.Out, f)); err == nil {
			t.Errorf("%s exists with the archive off", f)
		}
	}
}

func TestScanForbidden(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "feed.xml"), []byte("<a>ok</a>"), 0o644)
	os.WriteFile(filepath.Join(dir, "x.png"), []byte("172.16.1.1"), 0o644) // binary: not scanned
	c := config{Forbid: []string{`172\.16\.[0-9]+\.[0-9]+`}}
	if err := scanForbidden(c, dir); err != nil {
		t.Fatalf("clean site rejected: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "feed.xml"), []byte("<a>http://172.16.4.9/</a>"), 0o644)
	if err := scanForbidden(c, dir); err == nil {
		t.Error("forbidden address in the feed was published")
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestReportMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.prom")
	start := time.Now().Add(-5 * time.Second)
	if err := writeReportMetrics(path, start, nil, nil); err != nil {
		t.Fatal(err)
	}
	first := readGauge(path, "birdnet_report_last_success_timestamp_seconds")
	if first == 0 || readGauge(path, "birdnet_report_last_run_success") != 1 {
		t.Fatalf("success not recorded: %v", first)
	}
	// A failed run keeps the last success time and says it failed.
	if err := writeReportMetrics(path, time.Now(), nil, fmt.Errorf("boom")); err != nil {
		t.Fatal(err)
	}
	if readGauge(path, "birdnet_report_last_success_timestamp_seconds") != first || readGauge(path, "birdnet_report_last_run_success") != 0 {
		t.Error("a failed run overwrote the last success")
	}
}
