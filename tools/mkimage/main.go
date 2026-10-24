// mkimage builds the birdnet-snapshot container image without a container
// daemon: it takes the multi-arch chromedp/headless-shell image, adds one
// layer per platform (the binary, a CA bundle, a non-root user and /data) and
// sets the entrypoint. It runs on CI runners that have no Docker or Podman.
//
//	go run . -binary-dir ../../build -version v0.1.0 -repo ghcr.io/owner/birdnet-snapshot -push
//	go run . -binary-dir ../../build -version v0.1.0 -tarball out.tar -platform linux/amd64
//	go run . -check   # build with a placeholder binary, push nothing
package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	uid  = 10001
	user = "snapshot"
)

func main() {
	base := flag.String("base", "docker.io/chromedp/headless-shell:157.0.8103.15", "base image (multi-arch)")
	binDir := flag.String("binary-dir", "build", "directory with birdnet-snapshot_<version>_linux_<arch>/birdnet-snapshot")
	version := flag.String("version", "dev", "version tag (v1.2.3)")
	repo := flag.String("repo", "ghcr.io/parkwardrr/birdnet-snapshot", "image repository to push to")
	push := flag.Bool("push", false, "push the multi-arch index (credentials: REGISTRY_USER / REGISTRY_TOKEN)")
	tarOut := flag.String("tarball", "", "write one platform as a docker-load tarball instead")
	platform := flag.String("platform", "linux/amd64", "platform for -tarball")
	check := flag.Bool("check", false, "build every platform with a placeholder binary and push nothing")
	ca := flag.String("ca", "/etc/ssl/certs/ca-certificates.crt", "CA bundle to add (the base image has none)")
	flag.Parse()

	caPEM, err := os.ReadFile(*ca)
	if err != nil {
		log.Fatalf("CA bundle: %v", err)
	}
	baseRef, err := name.ParseReference(*base)
	if err != nil {
		log.Fatal(err)
	}
	idx, err := remote.Index(baseRef)
	if err != nil {
		log.Fatalf("base %s: %v", *base, err)
	}
	im, err := idx.IndexManifest()
	if err != nil {
		log.Fatal(err)
	}

	out := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	built := 0
	for _, desc := range im.Manifests {
		p := desc.Platform
		if p == nil || p.OS != "linux" || (p.Architecture != "amd64" && p.Architecture != "arm64") {
			continue
		}
		plat := p.OS + "/" + p.Architecture
		if *tarOut != "" && plat != *platform {
			continue
		}
		var bin []byte
		if *check {
			bin = []byte("#!/bin/sh\necho birdnet-snapshot placeholder\n")
		} else {
			path := filepath.Join(*binDir, fmt.Sprintf("birdnet-snapshot_%s_linux_%s", strings.TrimPrefix(*version, "v"), p.Architecture), "birdnet-snapshot")
			if bin, err = os.ReadFile(path); err != nil {
				log.Fatalf("%s binary: %v", plat, err)
			}
		}
		baseImg, err := idx.Image(desc.Digest)
		if err != nil {
			log.Fatalf("%s base: %v", plat, err)
		}
		img, err := build(baseImg, bin, caPEM, *version)
		if err != nil {
			log.Fatalf("%s: %v", plat, err)
		}
		d, _ := img.Digest()
		log.Printf("built %s %s", plat, d)
		built++
		if *tarOut != "" {
			tag, err := name.NewTag(*repo + ":" + strings.TrimPrefix(*version, "v"))
			if err != nil {
				log.Fatal(err)
			}
			if err := tarball.WriteToFile(*tarOut, tag, img); err != nil {
				log.Fatal(err)
			}
			log.Printf("wrote %s (%s)", *tarOut, tag)
			return
		}
		out = mutate.AppendManifests(out, mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{Platform: p}})
	}
	if built == 0 {
		log.Fatal("no linux/amd64 or linux/arm64 image in the base")
	}
	if *check || !*push {
		log.Printf("built %d platform(s); nothing pushed", built)
		return
	}

	auth := authn.Anonymous
	if u, t := os.Getenv("REGISTRY_USER"), os.Getenv("REGISTRY_TOKEN"); u != "" && t != "" {
		auth = &authn.Basic{Username: u, Password: t}
	} else {
		log.Fatal(errors.New("set REGISTRY_USER and REGISTRY_TOKEN to push"))
	}
	for _, t := range tags(*version) {
		ref, err := name.NewTag(*repo + ":" + t)
		if err != nil {
			log.Fatal(err)
		}
		if err := remote.WriteIndex(ref, out, remote.WithAuth(auth)); err != nil {
			log.Fatalf("push %s: %v", ref, err)
		}
		log.Printf("pushed %s", ref)
	}
}

// tags for v1.2.3: 1.2.3, 1.2, latest. Pre-releases get only their own tag.
func tags(version string) []string {
	v := strings.TrimPrefix(version, "v")
	if strings.Contains(v, "-") {
		return []string{v}
	}
	parts := strings.Split(v, ".")
	if len(parts) == 3 {
		return []string{v, parts[0] + "." + parts[1], "latest"}
	}
	return []string{v}
}

func build(base v1.Image, bin, caPEM []byte, version string) (v1.Image, error) {
	passwd, group, err := readFiles(base, "etc/passwd", "etc/group")
	if err != nil {
		return nil, err
	}
	passwd = append(ensureNL(passwd), []byte(fmt.Sprintf("%s:x:%d:%d::/data:/usr/sbin/nologin\n", user, uid, uid))...)
	group = append(ensureNL(group), []byte(fmt.Sprintf("%s:x:%d:\n", user, uid))...)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	now := time.Unix(0, 0)
	add := func(h *tar.Header, body []byte) error {
		h.ModTime = now
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	for _, f := range []struct {
		h    tar.Header
		body []byte
	}{
		{tar.Header{Name: "etc/passwd", Mode: 0o644, Typeflag: tar.TypeReg}, passwd},
		{tar.Header{Name: "etc/group", Mode: 0o644, Typeflag: tar.TypeReg}, group},
		{tar.Header{Name: "etc/ssl/certs/", Mode: 0o755, Typeflag: tar.TypeDir}, nil},
		{tar.Header{Name: "etc/ssl/certs/ca-certificates.crt", Mode: 0o644, Typeflag: tar.TypeReg}, caPEM},
		{tar.Header{Name: "usr/local/bin/birdnet-snapshot", Mode: 0o755, Typeflag: tar.TypeReg}, bin},
		{tar.Header{Name: "data/", Mode: 0o755, Typeflag: tar.TypeDir, Uid: uid, Gid: uid}, nil},
	} {
		h := f.h
		if err := add(&h, f.body); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	layerBytes := buf.Bytes()
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerBytes)), nil
	})
	if err != nil {
		return nil, err
	}
	img, err := mutate.AppendLayers(base, layer)
	if err != nil {
		return nil, err
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg := cf.Config.DeepCopy()
	cfg.Entrypoint = []string{"/usr/local/bin/birdnet-snapshot"}
	cfg.Cmd = []string{"run", "-every", "1h"}
	cfg.User = fmt.Sprintf("%d:%d", uid, uid)
	cfg.WorkingDir = "/data"
	cfg.Volumes = map[string]struct{}{"/data": {}}
	var env []string
	for _, e := range cfg.Env {
		if !strings.HasPrefix(e, "HOME=") {
			env = append(env, e)
		}
	}
	cfg.Env = append(env, "HOME=/data", "CHROME_PATH=/headless-shell/headless-shell", "CHROME_NO_SANDBOX=true", "SNAPSHOT_OUT=/data/site")
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}
	for k, v := range map[string]string{
		"org.opencontainers.image.title":       "birdnet-snapshot",
		"org.opencontainers.image.description": "Publish a read-only snapshot of a BirdNET-Go web UI to a CDN",
		"org.opencontainers.image.source":      "https://github.com/ParkWardRR/birdnet-snapshot",
		"org.opencontainers.image.licenses":    "BlueOak-1.0.0",
		"org.opencontainers.image.version":     version,
	} {
		cfg.Labels[k] = v
	}
	return mutate.Config(img, *cfg)
}

// readFiles returns the named files from an image's flattened filesystem.
func readFiles(img v1.Image, names ...string) ([]byte, []byte, error) {
	rc := mutate.Extract(img)
	defer rc.Close()
	found := map[string][]byte{}
	tr := tar.NewReader(rc)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		n := strings.TrimPrefix(h.Name, "./")
		for _, want := range names {
			if n == want {
				b, err := io.ReadAll(tr)
				if err != nil {
					return nil, nil, err
				}
				found[want] = b
			}
		}
	}
	return found[names[0]], found[names[1]], nil
}

func ensureNL(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] != '\n' {
		return append(b, '\n')
	}
	return b
}
