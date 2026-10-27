package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ntfy publishes push notifications to an ntfy topic (https://ntfy.sh, or a
// self-hosted server). The JSON publish API is used so titles and messages
// may hold any UTF-8.
type ntfy struct {
	URL   string // topic URL, e.g. https://ntfy.example.com/birds
	Token string // access token (Bearer), optional
	User  string // or basic-auth user and password, optional
	Pass  string
}

type ntfyMessage struct {
	Title    string
	Message  string
	Priority int // 1 (min) to 5 (max); 0 means the default, 3
	Tags     []string
	Click    string // URL opened when the notification is tapped
}

// enabled reports whether a topic is configured.
func (n ntfy) enabled() bool { return strings.TrimSpace(n.URL) != "" }

func (n ntfy) send(ctx context.Context, m ntfyMessage) error {
	u, err := url.Parse(n.URL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("ntfy: %q is not a topic URL", n.URL)
	}
	topic := strings.Trim(u.Path, "/")
	if topic == "" {
		return fmt.Errorf("ntfy: %q has no topic", n.URL)
	}
	body := map[string]any{"topic": topic, "title": m.Title, "message": m.Message}
	if m.Priority > 0 {
		body["priority"] = m.Priority
	}
	if len(m.Tags) > 0 {
		body["tags"] = m.Tags
	}
	if m.Click != "" {
		body["click"] = m.Click
	}
	b, _ := json.Marshal(body)
	root := u.Scheme + "://" + u.Host + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, root, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	switch {
	case n.Token != "":
		req.Header.Set("Authorization", "Bearer "+n.Token)
	case n.User != "":
		req.SetBasicAuth(n.User, n.Pass)
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ntfy: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// ntfyFlags are the notification settings every notifying command shares.
type ntfyFlags struct {
	ntfy
	Link string // BirdNET-Go's address as a phone sees it, for tap-through links
}

func ntfyFromEnv() ntfyFlags {
	return ntfyFlags{
		ntfy: ntfy{URL: env("NTFY_URL", ""), Token: env("NTFY_TOKEN", ""), User: env("NTFY_USER", ""), Pass: env("NTFY_PASSWORD", "")},
		Link: trimSlash(env("BIRDNET_LINK", "")),
	}
}
