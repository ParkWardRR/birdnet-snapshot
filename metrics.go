package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// runReport is one crawl+deploy, written as Prometheus textfile metrics.
type runReport struct {
	Start  time.Time
	Build  buildResult
	Deploy deployResult
	Err    error
}

// writeMetrics writes path atomically (temp file + rename) so the
// node_exporter textfile collector never reads a half-written file. The last
// success time survives failed runs by carrying it over from the old file.
func writeMetrics(path string, r *runReport) error {
	lastSuccess := readGauge(path, "birdnet_snapshot_last_success_timestamp_seconds")
	ok := 0
	if r.Err == nil {
		ok = 1
		lastSuccess = float64(time.Now().Unix())
	}
	var b strings.Builder
	g := func(name, help string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, strconv.FormatFloat(v, 'f', -1, 64))
	}
	fmt.Fprintf(&b, "# HELP birdnet_snapshot_info Build information.\n# TYPE birdnet_snapshot_info gauge\nbirdnet_snapshot_info{version=%q} 1\n", version)
	g("birdnet_snapshot_last_run_timestamp_seconds", "Unix time the last run started.", float64(r.Start.Unix()))
	g("birdnet_snapshot_last_success_timestamp_seconds", "Unix time of the last successful run.", lastSuccess)
	g("birdnet_snapshot_last_run_success", "1 if the last run published a snapshot.", float64(ok))
	g("birdnet_snapshot_run_duration_seconds", "Duration of the last run.", time.Since(r.Start).Seconds())
	g("birdnet_snapshot_crawl_duration_seconds", "Duration of the last crawl.", r.Build.CrawlDuration.Seconds())
	g("birdnet_snapshot_deploy_duration_seconds", "Duration of the last deploy.", r.Deploy.Duration.Seconds())
	g("birdnet_snapshot_pages", "Pages visited by the last crawl.", float64(r.Build.Pages))
	g("birdnet_snapshot_entries", "Recorded API responses in the last snapshot.", float64(r.Build.Entries))
	g("birdnet_snapshot_detections", "Detections found in the last snapshot.", float64(r.Build.Detections))
	g("birdnet_snapshot_clips", "Audio clips in the last snapshot.", float64(r.Build.Clips))
	g("birdnet_snapshot_files", "Files in the last snapshot.", float64(r.Build.Files))
	g("birdnet_snapshot_bytes", "Size of the last snapshot in bytes.", float64(r.Build.Bytes))
	g("birdnet_snapshot_uploaded_files", "Files uploaded by the last deploy.", float64(r.Deploy.Uploaded))
	g("birdnet_snapshot_uploaded_bytes", "Bytes uploaded by the last deploy.", float64(r.Deploy.Bytes))
	g("birdnet_snapshot_deleted_files", "Stale files deleted by the last deploy.", float64(r.Deploy.Deleted))

	return writeTextfile(path, b.String())
}

// readGauge returns the value of an unlabelled gauge in a textfile, or 0.
func readGauge(path, name string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), name+" "); ok {
			if x, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return x
			}
		}
	}
	return 0
}

// writeTextfile writes a node_exporter textfile atomically (temp file, then
// rename) so the collector never reads a half-written file. The temp name does
// not end in .prom, or the collector would read it.
func writeTextfile(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".birdnet-snapshot-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// writeReportMetrics writes the Field Notes run as textfile metrics (atomically,
// like writeMetrics). The last success time survives failed runs.
func writeReportMetrics(path string, start time.Time, out *reportOutcome, runErr error) error {
	lastSuccess := readGauge(path, "birdnet_report_last_success_timestamp_seconds")
	ok := 0
	if runErr == nil {
		ok = 1
		lastSuccess = float64(time.Now().Unix())
	}
	issues := readGauge(path, "birdnet_report_issues")
	if out != nil && out.store.enabled() {
		issues = float64(len(out.store.ids()))
	}
	var b strings.Builder
	g := func(name, help string, v float64) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, strconv.FormatFloat(v, 'f', -1, 64))
	}
	fmt.Fprintf(&b, "# HELP birdnet_report_info Build information.\n# TYPE birdnet_report_info gauge\nbirdnet_report_info{version=%q} 1\n", version)
	g("birdnet_report_last_run_timestamp_seconds", "Unix time the last Field Notes run started.", float64(start.Unix()))
	g("birdnet_report_last_success_timestamp_seconds", "Unix time of the last Field Notes run that built and published.", lastSuccess)
	g("birdnet_report_last_run_success", "1 if the last Field Notes run succeeded.", float64(ok))
	g("birdnet_report_run_duration_seconds", "Duration of the last Field Notes run.", time.Since(start).Seconds())
	g("birdnet_report_issues", "Weekly issues kept in the archive.", issues)
	return writeTextfile(path, b.String())
}
