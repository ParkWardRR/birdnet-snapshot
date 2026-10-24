package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// bunnyAPI is a minimal client for the Bunny account API (api.bunny.net).
type bunnyAPI struct {
	base   string
	key    string
	client *http.Client
}

func (a *bunnyAPI) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("AccessKey", a.key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(raw)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type storageZone struct {
	ID                 int64    `json:"Id"`
	Name               string   `json:"Name"`
	Password           string   `json:"Password"`
	Region             string   `json:"Region"`
	StorageHostname    string   `json:"StorageHostname"`
	ReplicationRegions []string `json:"ReplicationRegions"`
	Custom404FilePath  *string  `json:"Custom404FilePath"`
	Rewrite404To200    bool     `json:"Rewrite404To200"`
}

type pullZone struct {
	ID            int64  `json:"Id"`
	Name          string `json:"Name"`
	StorageZoneID int64  `json:"StorageZoneId"`
	Hostnames     []struct {
		Value          string `json:"Value"`
		HasCertificate bool   `json:"HasCertificate"`
		ForceSSL       bool   `json:"ForceSSL"`
	} `json:"Hostnames"`
	EdgeRules []struct {
		Guid             string `json:"Guid"`
		Description      string `json:"Description"`
		ActionParameter2 string `json:"ActionParameter2"`
		ActionType       int    `json:"ActionType"`
		ActionParameter1 string `json:"ActionParameter1"`
		Enabled          bool   `json:"Enabled"`
		Triggers         []struct {
			PatternMatches []string `json:"PatternMatches"`
		} `json:"Triggers"`
	} `json:"EdgeRules"`
}

type dnsZone struct {
	ID      int64  `json:"Id"`
	Domain  string `json:"Domain"`
	Records []struct {
		ID    int64  `json:"Id"`
		Type  int    `json:"Type"`
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"Records"`
}

// immutablePatterns are the content-addressed paths cached for a year.
var immutablePatterns = []string{"*/_snap/*", "*/ui/assets/*.js", "*/ui/assets/*.css"}

const (
	ruleEdge    = "birdnet-snapshot: immutable files, edge 1y"
	ruleBrowser = "birdnet-snapshot: immutable files, browser 1y"
)

type setupOpts struct {
	Name      string
	Region    string
	Replicate []string
	Hostname  string
	DNS       bool
	EnvFile   string
	EdgeTTL   int
	Delete    bool
	Purge     bool
	Yes       bool
	CertTries int
	CertWait  time.Duration
}

func bunnySetupMain(args []string) error {
	var o setupOpts
	fs := flag.NewFlagSet("bunny-setup", flag.ExitOnError)
	fs.StringVar(&o.Name, "name", env("BUNNY_STORAGE_ZONE", ""), "storage zone and pull zone `name` (globally unique at Bunny) [BUNNY_STORAGE_ZONE]")
	fs.StringVar(&o.Region, "region", "DE", "main storage `region`: DE, UK, SE, NY, LA, SG, SYD, BR, JH")
	replicate := fs.String("replicate", "", "extra storage replica `regions`, comma-separated (e.g. NY)")
	fs.StringVar(&o.Hostname, "hostname", "", "custom `hostname` for the snapshot, e.g. birds.example.com (optional)")
	fs.BoolVar(&o.DNS, "dns", true, "add the CNAME when the hostname's zone is hosted on Bunny DNS")
	fs.StringVar(&o.EnvFile, "env-file", "", "write BUNNY_STORAGE_* settings to this `file` (mode 600) instead of printing them")
	fs.IntVar(&o.EdgeTTL, "edge-ttl", 60, "edge cache `seconds` for entry files (index, snapshot.json)")
	fs.BoolVar(&o.Purge, "purge", false, "purge the pull zone's edge cache when done (after changing cache rules)")
	fs.BoolVar(&o.Delete, "delete", false, "delete the pull zone, storage zone and DNS record instead")
	fs.BoolVar(&o.Yes, "yes", false, "confirm -delete")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: birdnet-snapshot bunny-setup -name NAME [-hostname HOST] [flags]\n\nNeeds the account API key in BUNNY_API_KEY. Safe to re-run: existing zones are reused.\n\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	o.Replicate = splitList(strings.ToUpper(*replicate))
	o.Region = strings.ToUpper(o.Region)
	o.CertTries, o.CertWait = 6, 20*time.Second
	if o.Name == "" {
		fs.Usage()
		return errors.New("-name is required")
	}
	key := os.Getenv("BUNNY_API_KEY")
	if key == "" {
		return errors.New("set BUNNY_API_KEY to the account API key (Bunny dashboard → Account settings → API)")
	}
	api := &bunnyAPI{base: env("BUNNY_API_URL", "https://api.bunny.net"), key: key, client: &http.Client{Timeout: time.Minute}}
	ctx := context.Background()
	if o.Delete {
		return bunnyTeardown(ctx, api, o)
	}
	return bunnySetup(ctx, api, o, os.Stdout)
}

func bunnySetup(ctx context.Context, api *bunnyAPI, o setupOpts, out io.Writer) error {
	// 1. Storage zone.
	sz, err := findStorageZone(ctx, api, o.Name)
	if err != nil {
		return err
	}
	if sz == nil {
		log.Printf("creating storage zone %s in %s %v", o.Name, o.Region, o.Replicate)
		sz = &storageZone{}
		if err := api.do(ctx, http.MethodPost, "/storagezone", map[string]any{
			"Name": o.Name, "Region": o.Region, "ReplicationRegions": o.Replicate, "ZoneTier": 0,
		}, sz); err != nil {
			return err
		}
	} else {
		log.Printf("storage zone %s exists (id %d, %s)", sz.Name, sz.ID, sz.Region)
	}
	// SPA routes: anything missing serves the UI shell with status 200.
	if sz.Custom404FilePath == nil || *sz.Custom404FilePath != "/ui/index.html" || !sz.Rewrite404To200 {
		if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/storagezone/%d", sz.ID), map[string]any{
			"Custom404FilePath": "/ui/index.html", "Rewrite404To200": true,
		}, nil); err != nil {
			return err
		}
		log.Printf("storage zone: missing paths now serve /ui/index.html")
	}
	if sz.Password == "" || sz.StorageHostname == "" {
		if err := api.do(ctx, http.MethodGet, fmt.Sprintf("/storagezone/%d", sz.ID), nil, sz); err != nil {
			return err
		}
	}

	// 2. Pull zone in front of it.
	pz, err := findPullZone(ctx, api, o.Name)
	if err != nil {
		return err
	}
	if pz == nil {
		log.Printf("creating pull zone %s", o.Name)
		pz = &pullZone{}
		if err := api.do(ctx, http.MethodPost, "/pullzone", map[string]any{
			"Name": o.Name, "OriginType": 2, "StorageZoneId": sz.ID, "Type": 0,
			"EnableGeoZoneUS": true, "EnableGeoZoneEU": true, "EnableGeoZoneASIA": true,
			"EnableGeoZoneSA": true, "EnableGeoZoneAF": true,
		}, pz); err != nil {
			return err
		}
	} else {
		log.Printf("pull zone %s exists (id %d)", pz.Name, pz.ID)
		if pz.StorageZoneID != sz.ID {
			return fmt.Errorf("pull zone %s points at storage zone %d, not %d; pick another -name", pz.Name, pz.StorageZoneID, sz.ID)
		}
	}
	// Entry files short at the edge and never cached by browsers; query
	// strings ignored because the service worker, not the CDN, reads them.
	if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d", pz.ID), map[string]any{
		"CacheControlMaxAgeOverride": o.EdgeTTL, "CacheControlPublicMaxAgeOverride": 0,
		"IgnoreQueryStrings": true, "EnableTLS1": false, "EnableTLS1_1": false,
		"UseStaleWhileUpdating": true, "UseStaleWhileOffline": true,
	}, nil); err != nil {
		return err
	}
	// Only content-addressed paths are cached for a year: snapshot data and
	// Vite's hashed JS/CSS. Other UI assets (manifest, sounds, icons) keep
	// their names across BirdNET-Go versions, so they get the short TTL. An
	// existing rule with the same action is updated in place, whoever made it.
	want := strings.Join(immutablePatterns, " ")
	have := map[int]bool{}
	guids := map[int]string{}
	for _, r := range pz.EdgeRules {
		var pats []string
		for _, t := range r.Triggers {
			pats = append(pats, t.PatternMatches...)
		}
		if (r.ActionType == 3 || r.ActionType == 16) && r.ActionParameter1 == "31536000" && strings.Contains(strings.Join(pats, " "), "/_snap/") {
			guids[r.ActionType] = r.Guid
			have[r.ActionType] = r.Enabled && strings.Join(pats, " ") == want
		}
	}
	for _, rule := range []struct {
		desc   string
		action int
	}{{ruleEdge, 3}, {ruleBrowser, 16}} { // 3 = override cache time, 16 = override browser cache time
		if have[rule.action] {
			continue
		}
		body := map[string]any{
			"ActionType": rule.action, "ActionParameter1": "31536000", "Enabled": true, "Description": rule.desc,
			"TriggerMatchingType": 0,
			"Triggers":            []map[string]any{{"Type": 0, "PatternMatchingType": 0, "PatternMatches": immutablePatterns}},
		}
		verb := "added"
		if g := guids[rule.action]; g != "" {
			body["Guid"], verb = g, "updated"
		}
		if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/edgerules/addOrUpdate", pz.ID), body, nil); err != nil {
			return err
		}
		log.Printf("edge rule %s: %s", verb, rule.desc)
	}
	if err := ensureHardening(ctx, api, pz); err != nil {
		return err
	}
	cdnHost := o.Name + ".b-cdn.net"
	url := "https://" + cdnHost

	// 3. Custom hostname, DNS and certificate.
	if o.Hostname != "" {
		hostOK, certOK, sslOK := false, false, false
		for _, h := range pz.Hostnames {
			if strings.EqualFold(h.Value, o.Hostname) {
				hostOK, certOK, sslOK = true, h.HasCertificate, h.ForceSSL
			}
		}
		if !hostOK {
			if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/addHostname", pz.ID), map[string]any{"Hostname": o.Hostname}, nil); err != nil {
				return err
			}
			log.Printf("hostname %s added", o.Hostname)
		}
		if o.DNS {
			if err := ensureCNAME(ctx, api, o.Hostname, cdnHost); err != nil {
				log.Printf("dns: %v", err)
			}
		}
		if !certOK {
			if err := loadCert(ctx, api, o); err != nil {
				log.Printf("certificate: %v", err)
				log.Printf("point %s at %s (CNAME), then re-run bunny-setup to get the certificate", o.Hostname, cdnHost)
			} else {
				certOK = true
			}
		}
		if certOK && !sslOK {
			if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/setForceSSL", pz.ID), map[string]any{"Hostname": o.Hostname, "ForceSSL": true}, nil); err != nil {
				return err
			}
			log.Printf("HTTPS forced on %s", o.Hostname)
		}
		if certOK {
			url = "https://" + o.Hostname
		}
	}

	if o.Purge {
		if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/purgeCache", pz.ID), nil, nil); err != nil {
			return err
		}
		log.Printf("edge cache purged")
	}
	settings := fmt.Sprintf("BUNNY_STORAGE_ZONE=%s\nBUNNY_STORAGE_HOST=%s\nBUNNY_STORAGE_KEY=%s\n", sz.Name, sz.StorageHostname, sz.Password)
	if o.EnvFile != "" {
		if err := os.WriteFile(o.EnvFile, []byte(settings), 0o600); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s\n", o.EnvFile)
	} else {
		fmt.Fprintf(out, "\n# deploy settings (keep BUNNY_STORAGE_KEY secret)\n%s", settings)
	}
	fmt.Fprintf(out, "\nsnapshot URL: %s\n", url)
	return nil
}

func bunnyTeardown(ctx context.Context, api *bunnyAPI, o setupOpts) error {
	if !o.Yes {
		return fmt.Errorf("-delete removes pull zone, storage zone (and every file in it) and the DNS record for %q; add -yes to confirm", o.Name)
	}
	if pz, err := findPullZone(ctx, api, o.Name); err != nil {
		return err
	} else if pz != nil {
		if err := api.do(ctx, http.MethodDelete, fmt.Sprintf("/pullzone/%d", pz.ID), nil, nil); err != nil {
			return err
		}
		log.Printf("deleted pull zone %s", o.Name)
	}
	if sz, err := findStorageZone(ctx, api, o.Name); err != nil {
		return err
	} else if sz != nil {
		if err := api.do(ctx, http.MethodDelete, fmt.Sprintf("/storagezone/%d", sz.ID), nil, nil); err != nil {
			return err
		}
		log.Printf("deleted storage zone %s", o.Name)
	}
	if o.Hostname != "" {
		zone, sub, err := findDNSZone(ctx, api, o.Hostname)
		if err == nil && zone != nil {
			for _, r := range zone.Records {
				if r.Type == 2 && strings.EqualFold(r.Name, sub) && strings.EqualFold(strings.TrimSuffix(r.Value, "."), o.Name+".b-cdn.net") {
					if err := api.do(ctx, http.MethodDelete, fmt.Sprintf("/dnszone/%d/records/%d", zone.ID, r.ID), nil, nil); err != nil {
						return err
					}
					log.Printf("deleted CNAME %s", o.Hostname)
				}
			}
		}
	}
	return nil
}

func findStorageZone(ctx context.Context, api *bunnyAPI, name string) (*storageZone, error) {
	var zones []storageZone
	if err := api.do(ctx, http.MethodGet, "/storagezone", nil, &zones); err != nil {
		return nil, err
	}
	for i := range zones {
		if strings.EqualFold(zones[i].Name, name) {
			return &zones[i], nil
		}
	}
	return nil, nil
}

func findPullZone(ctx context.Context, api *bunnyAPI, name string) (*pullZone, error) {
	var zones []pullZone
	if err := api.do(ctx, http.MethodGet, "/pullzone", nil, &zones); err != nil {
		return nil, err
	}
	for i := range zones {
		if strings.EqualFold(zones[i].Name, name) {
			return &zones[i], nil
		}
	}
	return nil, nil
}

// findDNSZone returns the Bunny DNS zone holding host and the record name
// within it, or nil when the domain is not hosted on Bunny DNS.
func findDNSZone(ctx context.Context, api *bunnyAPI, host string) (*dnsZone, string, error) {
	var page struct {
		Items []dnsZone `json:"Items"`
	}
	if err := api.do(ctx, http.MethodGet, "/dnszone?perPage=1000", nil, &page); err != nil {
		return nil, "", err
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	var best *dnsZone
	for i := range page.Items {
		d := strings.ToLower(page.Items[i].Domain)
		if host == d || strings.HasSuffix(host, "."+d) {
			if best == nil || len(d) > len(best.Domain) {
				best = &page.Items[i]
			}
		}
	}
	if best == nil {
		return nil, "", nil
	}
	var full dnsZone
	if err := api.do(ctx, http.MethodGet, fmt.Sprintf("/dnszone/%d", best.ID), nil, &full); err != nil {
		return nil, "", err
	}
	sub := strings.TrimSuffix(strings.TrimSuffix(host, strings.ToLower(full.Domain)), ".")
	return &full, sub, nil
}

func ensureCNAME(ctx context.Context, api *bunnyAPI, host, target string) error {
	zone, sub, err := findDNSZone(ctx, api, host)
	if err != nil {
		return err
	}
	if zone == nil {
		return fmt.Errorf("%s is not on Bunny DNS: create a CNAME %s → %s at your DNS provider", host, host, target)
	}
	if sub == "" {
		return fmt.Errorf("%s is a zone apex; a CNAME can't go there, use a subdomain", host)
	}
	for _, r := range zone.Records {
		if strings.EqualFold(r.Name, sub) {
			if r.Type == 2 && strings.EqualFold(strings.TrimSuffix(r.Value, "."), target) {
				log.Printf("dns: CNAME %s → %s exists", host, target)
				return nil
			}
			return fmt.Errorf("%s already has a different record (type %d → %s); not touching it", host, r.Type, r.Value)
		}
	}
	if err := api.do(ctx, http.MethodPut, fmt.Sprintf("/dnszone/%d/records", zone.ID), map[string]any{
		"Type": 2, "Name": sub, "Value": target, "Ttl": 300,
	}, nil); err != nil {
		return err
	}
	log.Printf("dns: CNAME %s → %s added", host, target)
	return nil
}

// loadCert asks Bunny for a free certificate, retrying while DNS propagates.
func loadCert(ctx context.Context, api *bunnyAPI, o setupOpts) error {
	var err error
	for i := 0; i < o.CertTries; i++ {
		if err = api.do(ctx, http.MethodGet, "/pullzone/loadFreeCertificate?hostname="+o.Hostname, nil, nil); err == nil {
			log.Printf("certificate issued for %s", o.Hostname)
			return nil
		}
		if i < o.CertTries-1 {
			log.Printf("certificate not ready (%v); retrying in %s", err, o.CertWait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(o.CertWait):
			}
		}
	}
	return err
}

// securityHeaders are added to every response at the edge. The page itself
// carries a stricter Content-Security-Policy (with script hashes) as a meta
// tag; frame-ancestors only works as a header.
var securityHeaders = []struct{ Name, Value string }{
	{"Strict-Transport-Security", "max-age=31536000"},
	{"X-Content-Type-Options", "nosniff"},
	{"X-Frame-Options", "DENY"},
	{"Referrer-Policy", "no-referrer"},
	{"Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()"},
	{"Cross-Origin-Opener-Policy", "same-origin"},
	{"Cross-Origin-Resource-Policy", "same-origin"},
	{"Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"},
}

const ruleBlockMethods = "birdnet-snapshot: read-only, block writes"
const ruleManifestType = "birdnet-snapshot: web manifest content type"

// ensureHardening adds the security headers and blocks every method but
// GET/HEAD at the edge. Existing rules are matched by what they do, and
// header values are updated in place when they differ.
func ensureHardening(ctx context.Context, api *bunnyAPI, pz *pullZone) error {
	for _, h := range securityHeaders {
		guid, same := "", false
		for _, r := range pz.EdgeRules {
			if r.ActionType == 5 && strings.EqualFold(r.ActionParameter1, h.Name) {
				guid, same = r.Guid, r.ActionParameter2 == h.Value && r.Enabled
			}
		}
		if same {
			continue
		}
		rule := map[string]any{
			"ActionType": 5, "ActionParameter1": h.Name, "ActionParameter2": h.Value, "Enabled": true,
			"Description": "birdnet-snapshot: " + h.Name, "TriggerMatchingType": 0,
			"Triggers": []map[string]any{{"Type": 0, "PatternMatchingType": 0, "PatternMatches": []string{"*"}}},
		}
		if guid != "" {
			rule["Guid"] = guid
		}
		if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/edgerules/addOrUpdate", pz.ID), rule, nil); err != nil {
			return err
		}
		log.Printf("header set at the edge: %s", h.Name)
	}
	// Storage serves .webmanifest as application/octet-stream, which nosniff
	// would make browsers reject.
	manifestOK := false
	for _, r := range pz.EdgeRules {
		if r.Description == ruleManifestType {
			manifestOK = true
		}
	}
	if !manifestOK {
		if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/edgerules/addOrUpdate", pz.ID), map[string]any{
			"ActionType": 5, "ActionParameter1": "Content-Type", "ActionParameter2": "application/manifest+json", "Enabled": true,
			"Description": ruleManifestType, "TriggerMatchingType": 0,
			"Triggers": []map[string]any{{"Type": 0, "PatternMatchingType": 0, "PatternMatches": []string{"*.webmanifest"}}},
		}, nil); err != nil {
			return err
		}
		log.Printf("edge rule added: %s", ruleManifestType)
	}
	for _, r := range pz.EdgeRules {
		if r.ActionType == 4 && r.Description == ruleBlockMethods {
			return nil
		}
	}
	// 4 = block request; trigger 9 = request method.
	if err := api.do(ctx, http.MethodPost, fmt.Sprintf("/pullzone/%d/edgerules/addOrUpdate", pz.ID), map[string]any{
		"ActionType": 4, "Enabled": true, "Description": ruleBlockMethods, "TriggerMatchingType": 0,
		"Triggers": []map[string]any{{"Type": 9, "PatternMatchingType": 0, "PatternMatches": []string{"POST", "PUT", "PATCH", "DELETE"}}},
	}, nil); err != nil {
		return err
	}
	log.Printf("edge rule added: %s", ruleBlockMethods)
	return nil
}
