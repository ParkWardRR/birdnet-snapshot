package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Issues are Field Notes' weekly editions. The page at the site root is the
// rolling report for the last days; once an ISO week (Monday to Sunday) is over
// it is frozen as an issue that never changes again, and the archive page and
// the Atom feed list them. Issues are kept in a store directory of their own,
// because the site is rebuilt from scratch every hour.
//
//	<store>/2026-w43/issue.json   the report data, as it was when the week closed
//	<store>/2026-w43/_snap/...    its photos, recordings and PDF

// reportIssue marks a report as an issue.
type reportIssue struct {
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Published string `json:"published"`
	Prev      string `json:"prev,omitempty"`
	Next      string `json:"next,omitempty"`
}

// issueEntry is one line of the archive.
type issueEntry struct {
	ID         string `json:"id"`
	Number     int    `json:"number"`
	Label      string `json:"label"`
	Start      string `json:"start"`
	End        string `json:"end"`
	Published  string `json:"published"`
	Detections int    `json:"detections"`
	Species    int    `json:"species"`
	Top        string `json:"top,omitempty"`
	TopPhoto   string `json:"topPhoto,omitempty"`
	Summary    string `json:"summary,omitempty"`
	PDF        string `json:"pdf,omitempty"`

	highlights []string
}

type issueIndex struct {
	Issues []issueEntry `json:"issues"` // newest first
}

const issueSettle = time.Hour // wait this long after a week ends before freezing it

// issueID names an ISO week, e.g. 2026-w43.
func issueID(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-w%02d", y, w)
}

// weekStart is the Monday of t's ISO week, at midnight in t's zone.
func weekStart(t time.Time) time.Time {
	d := t.AddDate(0, 0, -(int(t.Weekday()+6) % 7))
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}

// completedWeeks returns the Mondays of the weeks, from the one containing
// first, that are over (and settled) at now. At most max of the latest.
func completedWeeks(first, now time.Time, max int) []time.Time {
	var out []time.Time
	for w := weekStart(first); !now.Before(w.AddDate(0, 0, 7).Add(issueSettle)); w = w.AddDate(0, 0, 7) {
		out = append(out, w)
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

type issueStore struct{ dir string }

func (s issueStore) enabled() bool { return s.dir != "" }

// ids lists the stored issues, oldest first.
func (s issueStore) ids() []string {
	ents, _ := os.ReadDir(s.dir)
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(s.dir, e.Name(), "issue.json")); err == nil {
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// announced reports whether the issue's digest has been sent.
func (s issueStore) announced(id string) bool {
	_, err := os.Stat(filepath.Join(s.dir, id, "announced"))
	return err == nil
}

func (s issueStore) markAnnounced(id string) error {
	return os.WriteFile(filepath.Join(s.dir, id, "announced"), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
}

// ensure freezes every finished week that has no issue yet (the last max
// weeks, since the station first heard anything) and returns the new ones.
// A week that fails is logged and left for the next run.
func (s issueStore) ensure(ctx context.Context, c config, rc reportConfig, loc *time.Location, now time.Time) []string {
	first, err := stationStart(ctx, c, loc, now)
	if err != nil {
		log.Printf("report: issues: %v", err)
		return nil
	}
	have := map[string]bool{}
	for _, id := range s.ids() {
		have[id] = true
	}
	var created []string
	for _, monday := range completedWeeks(first, now, rc.IssueWeeks) {
		id := issueID(monday)
		if have[id] {
			continue
		}
		if err := s.build(ctx, c, rc, loc, now, monday, len(have)+1); err != nil {
			if strings.Contains(err.Error(), "no detections") {
				continue // a week the station was silent or off
			}
			log.Printf("report: issue %s: %v", id, err)
			continue
		}
		have[id] = true
		created = append(created, id)
	}
	return created
}

// stationStart is the day of the station's first detection.
func stationStart(ctx context.Context, c config, loc *time.Location, now time.Time) (time.Time, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	b, _, err := get(ctx, client, c.BirdNET+"/api/v2/analytics/species/summary?start_date=2000-01-01&end_date="+now.Format("2006-01-02"))
	if err != nil {
		return time.Time{}, err
	}
	var rows []struct {
		First string `json:"first_heard"`
	}
	if err := json.Unmarshal(b, &rows); err != nil {
		return time.Time{}, err
	}
	var first time.Time
	for _, r := range rows {
		if t, err := time.Parse(time.RFC3339, r.First); err == nil && (first.IsZero() || t.Before(first)) {
			first = t
		}
	}
	if first.IsZero() {
		return time.Time{}, fmt.Errorf("the station has heard nothing yet")
	}
	return first.In(loc), nil
}

// build collects the week starting monday, writes it as a site (with its PDF)
// in a scratch directory and keeps its data and files as the issue.
func (s issueStore) build(ctx context.Context, c config, rc reportConfig, loc *time.Location, now, monday time.Time, number int) error {
	id := issueID(monday)
	d, dets, err := collectReport(ctx, c, rc, loc, monday, monday.AddDate(0, 0, 6), now)
	if err != nil {
		return err
	}
	d.Issue = &reportIssue{ID: id, Number: number, Published: now.Format(time.RFC3339)}
	scratch := filepath.Join(s.dir, id+".tmp")
	defer os.RemoveAll(scratch)
	if err := writeReportSite(ctx, c, rc, d, dets, scratch); err != nil {
		return err
	}
	final := filepath.Join(s.dir, id)
	if err := os.RemoveAll(final); err != nil {
		return err
	}
	if err := os.MkdirAll(final, 0o755); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(scratch, "_snap"), filepath.Join(final, "_snap")); err != nil {
		return err
	}
	// The data goes last: an issue exists only once issue.json does.
	if err := os.Rename(filepath.Join(scratch, "data", "report.json"), filepath.Join(final, "issue.json")); err != nil {
		return err
	}
	log.Printf("report: issue %s (No. %d, %s) frozen", id, number, d.Period.Label)
	return nil
}

// assemble puts every stored issue into the site: its data under
// data/issues/, its files under _snap/, and data/issues.json listing them.
// Numbers and neighbours come from the order of the store, so they stay right
// when weeks are added later. Files are hard-linked when possible: a year of
// issues is about a gigabyte that is rebuilt into the site every hour.
func (s issueStore) assemble(site string, feed bool) (*issueIndex, error) {
	ids := s.ids()
	idx := &issueIndex{}
	if err := os.MkdirAll(filepath.Join(site, "data", "issues"), 0o755); err != nil {
		return nil, err
	}
	for i, id := range ids {
		raw, err := os.ReadFile(filepath.Join(s.dir, id, "issue.json"))
		if err != nil {
			return nil, err
		}
		var d reportData
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("issue %s: %w", id, err)
		}
		pub := ""
		if d.Issue != nil {
			pub = d.Issue.Published
		}
		meta := &reportIssue{ID: id, Number: i + 1, Published: pub}
		if i > 0 {
			meta.Prev = ids[i-1]
		}
		if i+1 < len(ids) {
			meta.Next = ids[i+1]
		}
		// Patch the issue marker into the stored JSON without disturbing the rest.
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		doc["issue"] = meta
		doc["issueCount"] = len(ids)
		if feed {
			doc["feed"] = true
		}
		out, _ := json.Marshal(doc)
		if err := os.WriteFile(filepath.Join(site, "data", "issues", id+".json"), out, 0o644); err != nil {
			return nil, err
		}
		if err := linkTree(filepath.Join(s.dir, id, "_snap"), filepath.Join(site, "_snap")); err != nil {
			return nil, err
		}
		e := issueEntry{ID: id, Number: i + 1, Label: d.Period.Label, Start: d.Period.Start, End: d.Period.End, Published: pub,
			Detections: d.Totals.Detections, Species: d.Totals.Species, PDF: d.PDF, highlights: d.Highlights}
		if len(d.Species) > 0 {
			e.Top = d.Species[0].Common
			if p := d.Species[0].Photo; p != nil {
				e.TopPhoto = p.Small
			}
		}
		if n := len(d.Highlights); n > 0 {
			e.Summary = strings.Join(d.Highlights[:min(n, 3)], " ")
		}
		idx.Issues = append([]issueEntry{e}, idx.Issues...)
	}
	b, _ := json.Marshal(idx)
	return idx, os.WriteFile(filepath.Join(site, "data", "issues.json"), b, 0o644)
}

// linkTree hard-links (or copies) every file of src into dst.
func linkTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue // the same content-addressed file is already there
		}
		if err := os.Link(from, to); err != nil {
			b, rerr := os.ReadFile(from)
			if rerr != nil {
				return rerr
			}
			if werr := os.WriteFile(to, b, 0o644); werr != nil {
				return werr
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Atom feed

type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel    string `xml:"rel,attr,omitempty"`
	Type   string `xml:"type,attr,omitempty"`
	Href   string `xml:"href,attr"`
	Length int    `xml:"length,attr,omitempty"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Links   []atomLink `xml:"link"`
	Summary string     `xml:"summary"`
}

// writeFeed writes feed.xml: one entry per issue, newest first, with absolute
// links (site is the public address of the report).
func writeFeed(dir, site, title, subtitle string, idx *issueIndex, now time.Time) error {
	site = strings.TrimRight(site, "/")
	host := strings.TrimPrefix(strings.TrimPrefix(site, "https://"), "http://")
	host, _, _ = strings.Cut(host, "/")
	f := atomFeed{
		Title: title, Subtitle: subtitle, ID: site + "/", Updated: now.Format(time.RFC3339),
		Links: []atomLink{{Rel: "self", Type: "application/atom+xml", Href: site + "/feed.xml"}, {Rel: "alternate", Type: "text/html", Href: site + "/"}},
	}
	if len(idx.Issues) > 0 {
		f.Updated = idx.Issues[0].Published
	}
	for i, e := range idx.Issues {
		if i == 30 {
			break
		}
		entry := atomEntry{
			Title:   fmt.Sprintf("%s No. %d: %s", title, e.Number, e.Label),
			ID:      fmt.Sprintf("tag:%s,%s:field-notes:%s", host, strings.SplitN(e.Published, "T", 2)[0], e.ID),
			Updated: e.Published,
			Links:   []atomLink{{Rel: "alternate", Type: "text/html", Href: site + "/?issue=" + e.ID}},
			Summary: fmt.Sprintf("%s detections of %d species. %s", commas(e.Detections), e.Species, e.Summary),
		}
		if e.PDF != "" {
			entry.Links = append(entry.Links, atomLink{Rel: "enclosure", Type: "application/pdf", Href: site + e.PDF})
		}
		f.Entries = append(f.Entries, entry)
	}
	b, err := xml.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "feed.xml"), append([]byte(xml.Header), b...), 0o644)
}

// ---------------------------------------------------------------------------
// Link previews

// injectHead adds the link-preview and feed-discovery tags to the page's
// <head>. Crawlers don't run the page's script, so they must be in the HTML.
func injectHead(dir, site, title, description, image string, feed bool) error {
	path := filepath.Join(dir, "index.html")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	site = strings.TrimRight(site, "/")
	esc := func(s string) string { var sb strings.Builder; _ = xml.EscapeText(&sb, []byte(s)); return sb.String() }
	var meta strings.Builder
	fmt.Fprintf(&meta, "\t\t<meta property=\"og:type\" content=\"website\" />\n\t\t<meta property=\"og:title\" content=\"%s\" />\n\t\t<meta property=\"og:description\" content=\"%s\" />\n\t\t<meta property=\"og:url\" content=\"%s/\" />\n",
		esc(title), esc(description), esc(site))
	if image != "" {
		fmt.Fprintf(&meta, "\t\t<meta property=\"og:image\" content=\"%s%s\" />\n\t\t<meta property=\"og:image:width\" content=\"1200\" />\n\t\t<meta property=\"og:image:height\" content=\"630\" />\n\t\t<meta name=\"twitter:card\" content=\"summary_large_image\" />\n",
			esc(site), esc(image))
	}
	if feed {
		fmt.Fprintf(&meta, "\t\t<link rel=\"alternate\" type=\"application/atom+xml\" title=\"%s\" href=\"feed.xml\" />\n", esc(title))
	}
	out := strings.Replace(string(b), "</head>", meta.String()+"\t</head>", 1)
	if out == string(b) {
		return fmt.Errorf("index.html has no </head>")
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// ---------------------------------------------------------------------------
// Digest

// issueDigest is the push notification for a new issue.
func issueDigest(e issueEntry, site string) ntfyMessage {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s detections of %d species.\n", e.Label, commas(e.Detections), e.Species)
	for i, h := range e.highlights {
		if i == 4 {
			break
		}
		b.WriteString("• " + h + "\n")
	}
	m := ntfyMessage{Title: fmt.Sprintf("Field Notes No. %d is out", e.Number), Message: strings.TrimSpace(b.String()), Tags: []string{"bird", "newspaper"}}
	if site != "" {
		m.Click = strings.TrimRight(site, "/") + "/?issue=" + e.ID
	}
	return m
}

// scanForbidden applies -forbid to every text file of the built site.
func scanForbidden(c config, dir string) error {
	red, err := newRedactor(c)
	if err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(p) {
		case ".json", ".xml", ".html":
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			return red.check(filepath.ToSlash(rel), b)
		}
		return nil
	})
}
