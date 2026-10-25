package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"
)

// serveHandler serves dir the way the Bunny pull zone does: files by path,
// query strings ignored, and /ui/index.html for anything missing.
func serveHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil || fi.IsDir() {
			if p == "/" {
				p = "/index.html"
			} else {
				p = "/ui/index.html"
			}
		}
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, p, fi.ModTime(), f) // no index.html redirect, unlike FileServer
	})
}

// serve previews c.Out locally until the context ends.
func serve(ctx context.Context, c config, addr string) error {
	h := serveHandler(c.Out)
	if _, err := os.Stat(filepath.Join(c.Out, "ui", "index.html")); err != nil {
		h = reportServeHandler(c.Out) // a report, not a snapshot
	}
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("serving %s on http://%s", c.Out, addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
