package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// printReportPDF renders the built report in headless Chrome and saves it as
// a PDF under _snap/ (content-addressed), returning its path relative to dir.
// The page sets <html data-ready="1"> once its data and images have loaded,
// and its print stylesheet lays it out in pages.
func printReportPDF(ctx context.Context, c config, dir string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: reportServeHandler(dir), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var alloc context.Context
	var acancel context.CancelFunc
	if c.Chrome != "" {
		alloc, acancel = chromedp.NewRemoteAllocator(ctx, c.Chrome)
	} else {
		opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1280, 1600))
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

	var pdf []byte
	err = chromedp.Run(tab,
		chromedp.Navigate("http://"+ln.Addr().String()+"/?print=1"),
		chromedp.Poll(`document.documentElement.dataset.ready === '1'`, nil, chromedp.WithPollingTimeout(90*time.Second)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithGenerateDocumentOutline(true).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pdf)
	name := fmt.Sprintf("_snap/field-notes-%s.pdf", hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(filepath.Join(dir, "_snap"), 0o755); err != nil {
		return "", err
	}
	return name, os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), pdf, 0o644)
}
