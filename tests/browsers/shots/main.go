// shots takes screenshots of page elements (Chromium, 2x), for README images.
//
//	go run ./shots -url http://127.0.0.1:8771 -out /tmp/shots \
//	    'weather=.chart:has(h3:text-is("Weather and song"))' \
//	    'species=click:.cards .card >> dialog'
//
// Each argument is name=selector. A selector of the form click:A >> B clicks A
// first, then photographs B (a dialog, say). The page's floating buttons are
// hidden, and the page must set html[data-ready="1"] (Field Notes does).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8771", "page to photograph")
	out := flag.String("out", "shots", "output directory")
	width := flag.Int("width", 1100, "viewport width")
	height := flag.Int("height", 1000, "viewport height")
	flag.Parse()
	if flag.NArg() == 0 {
		log.Fatal("usage: shots -url URL -out DIR name=selector ...")
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:          &playwright.Size{Width: *width, Height: *height},
		DeviceScaleFactor: playwright.Float(2),
	})
	if err != nil {
		log.Fatal(err)
	}
	page, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := page.Goto(*url); err != nil {
		log.Fatal(err)
	}
	if err := page.Locator(`html[data-ready="1"]`).WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateAttached, Timeout: playwright.Float(30000)}); err != nil {
		log.Fatalf("the page never became ready: %v", err)
	}
	_, _ = page.AddStyleTag(playwright.PageAddStyleTagOptions{Content: playwright.String(".actions{display:none !important}")})

	for _, arg := range flag.Args() {
		name, sel, ok := strings.Cut(arg, "=")
		if !ok {
			log.Fatalf("%q is not name=selector", arg)
		}
		if click, rest, ok := strings.Cut(sel, " >> "); ok && strings.HasPrefix(click, "click:") {
			if err := page.Locator(strings.TrimPrefix(click, "click:")).First().Click(); err != nil {
				log.Fatalf("%s: click: %v", name, err)
			}
			time.Sleep(600 * time.Millisecond)
			sel = rest
		}
		loc := page.Locator(sel).First()
		if err := loc.ScrollIntoViewIfNeeded(); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		time.Sleep(500 * time.Millisecond) // lazy images, chart widths
		path := filepath.Join(*out, name+".png")
		if _, err := loc.Screenshot(playwright.LocatorScreenshotOptions{Path: playwright.String(path)}); err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		fmt.Println(path)
	}
}
