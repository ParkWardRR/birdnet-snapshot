package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// check tests every external dependency and prints one line per check.
func check(ctx context.Context, c config) error {
	failed := 0
	report := func(name string, err error, detail string) {
		if err != nil {
			failed++
			fmt.Printf("  ✗ %-10s %v\n", name, err)
			return
		}
		fmt.Printf("  ✓ %-10s %s\n", name, detail)
	}
	fmt.Println("birdnet-snapshot", version, "check")

	detail, err := checkBirdNET(ctx, c.BirdNET)
	report("birdnet", err, detail)

	detail, err = checkChrome(ctx, c)
	report("chrome", err, detail)

	if c.Zone != "" || c.ZoneKey != "" {
		detail, err = checkBunny(ctx, c)
		report("bunny", err, detail)
	} else {
		fmt.Printf("  - %-10s not configured (BUNNY_STORAGE_ZONE / BUNNY_STORAGE_KEY); crawl and serve still work\n", "bunny")
	}

	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

func checkBirdNET(ctx context.Context, base string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	body, _, err := get(ctx, client, base+"/api/v2/app/config")
	if err != nil {
		return "", fmt.Errorf("%s: %w", base, err)
	}
	var cfg struct {
		Version  string `json:"version"`
		Security struct {
			Enabled     bool `json:"enabled"`
			PrivateMode bool `json:"privateMode"`
		} `json:"security"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", fmt.Errorf("app/config is not JSON; is %s BirdNET-Go?", base)
	}
	shellHTML, _, err := get(ctx, client, base+"/ui/")
	if err != nil {
		return "", err
	}
	if _, err := shell(shellHTML, false); err != nil {
		return "", err
	}
	d := fmt.Sprintf("%s, BirdNET-Go %s", base, cfg.Version)
	if cfg.Security.PrivateMode {
		return "", fmt.Errorf("%s has private mode on: guests see no data, so a snapshot would be empty", base)
	}
	if cfg.Security.Enabled {
		d += ", auth on (guest view is recorded)"
	}
	return d, nil
}

func checkChrome(ctx context.Context, c config) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var alloc context.Context
	var acancel context.CancelFunc
	where := "launched"
	if c.Chrome != "" {
		alloc, acancel = chromedp.NewRemoteAllocator(ctx, c.Chrome)
		where = c.Chrome
	} else {
		opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1440, 2400))
		if c.ChromePath != "" {
			opts = append(opts, chromedp.ExecPath(c.ChromePath))
		}
		if c.NoSandbox {
			opts = append(opts, chromedp.NoSandbox)
		}
		alloc, acancel = chromedp.NewExecAllocator(ctx, opts...)
	}
	defer acancel()
	tab, tcancel := chromedp.NewContext(alloc)
	defer tcancel()
	var product string
	err := chromedp.Run(tab, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		_, product, _, _, _, err = browser.GetVersion().Do(ctx)
		return err
	}))
	if err != nil {
		return "", fmt.Errorf("cannot start or reach Chrome (%s): %w", where, err)
	}
	return product + " (" + where + ")", nil
}

func checkBunny(ctx context.Context, c config) (string, error) {
	b, err := newBunny(c)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.base, nil)
	req.Header.Set("AccessKey", b.key)
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("zone %q rejected the key (use the storage zone password, not the account API key)", c.Zone)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("zone %q: %s %s", c.Zone, resp.Status, strings.TrimSpace(string(raw)))
	}
	d := fmt.Sprintf("zone %s on %s", c.Zone, c.ZoneHost)
	if info, _, err := b.previous(ctx); err == nil && info.GeneratedAt > 0 {
		age := time.Since(time.UnixMilli(info.GeneratedAt)).Round(time.Minute)
		d += fmt.Sprintf(", live snapshot %s old (%d entries)", age, info.Entries)
	} else {
		d += ", nothing published yet"
	}
	return d, nil
}
