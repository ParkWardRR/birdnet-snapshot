package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStorage is an in-memory Bunny storage zone API.
type fakeStorage struct {
	mu    sync.Mutex
	zone  string
	key   string
	files map[string][]byte
	puts  []string
	dels  []string
}

func newFakeStorage(t *testing.T) (*fakeStorage, *httptest.Server) {
	f := &fakeStorage{zone: "zone", key: "secret", files: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeStorage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("AccessKey") != f.key {
		http.Error(w, `{"HttpCode":401,"Message":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	rel, ok := strings.CutPrefix(r.URL.Path, "/"+f.zone+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if rel == "" || strings.HasSuffix(rel, "/") {
			type obj struct {
				ObjectName  string
				Path        string
				IsDirectory bool
				Checksum    string
			}
			seen := map[string]bool{}
			out := []obj{}
			for name, b := range f.files {
				rest, ok := strings.CutPrefix(name, rel)
				if !ok {
					continue
				}
				if dir, _, isDir := strings.Cut(rest, "/"); isDir {
					if !seen[dir] {
						seen[dir] = true
						out = append(out, obj{ObjectName: dir, IsDirectory: true})
					}
					continue
				}
				sum := sha256.Sum256(b)
				out = append(out, obj{ObjectName: rest, Checksum: strings.ToUpper(hex.EncodeToString(sum[:]))})
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		b, ok := f.files[rel]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(b)
		if r.Header.Get("Checksum") != strings.ToUpper(hex.EncodeToString(sum[:])) {
			http.Error(w, "checksum mismatch", http.StatusBadRequest)
			return
		}
		f.files[rel] = b
		f.puts = append(f.puts, rel)
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		delete(f.files, rel)
		f.dels = append(f.dels, rel)
	}
}

func (f *fakeStorage) reset() {
	f.mu.Lock()
	f.puts, f.dels = nil, nil
	f.mu.Unlock()
}

func deployConfig(srv *httptest.Server, out string) config {
	return config{Out: out, Zone: "zone", ZoneKey: "secret", ZoneHost: srv.URL, Concurrency: 4, MaxShrink: 0.5}
}

func TestDeployUploadsEntryFilesLast(t *testing.T) {
	f, srv := newFakeStorage(t)
	_, dir := buildTestSite(t, nil)
	res, err := deploy(context.Background(), deployConfig(srv, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.puts) == 0 || f.puts[len(f.puts)-1] != "snapshot.json" {
		t.Fatalf("snapshot.json must be uploaded last, order: %v", f.puts)
	}
	idx := map[string]int{}
	for i, p := range f.puts {
		idx[p] = i
	}
	for _, p := range f.puts {
		if strings.HasPrefix(p, "_snap/") && idx[p] > idx["ui/index.html"] {
			t.Errorf("%s uploaded after the entry files", p)
		}
	}
	if res.Uploaded != len(f.puts) || res.Deleted != 0 {
		t.Errorf("bad result %+v", res)
	}

	// Unchanged site: nothing to do.
	f.reset()
	res, err = deploy(context.Background(), deployConfig(srv, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.puts) != 0 || res.Uploaded != 0 {
		t.Errorf("re-deploy uploaded %v", f.puts)
	}
}

func TestDeployKeepsOneGenerationOfGrace(t *testing.T) {
	f, srv := newFakeStorage(t)
	dir := filepath.Join(t.TempDir(), "site")
	gen := func(body string) {
		st := newTestStore()
		st.index = []byte(testShell)
		st.put(&capture{Key: "/api/v2/app/config", MIME: "application/json", Body: []byte(`{}`)})
		st.put(&capture{Key: "/api/v2/detections/recent", MIME: "application/json", Body: []byte(body)})
		if _, err := buildSite(config{Out: dir}, st, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := deploy(context.Background(), deployConfig(srv, dir)); err != nil {
			t.Fatal(err)
		}
	}
	snapFiles := func() []string {
		f.mu.Lock()
		defer f.mu.Unlock()
		var out []string
		for k := range f.files {
			if strings.HasPrefix(k, "_snap/") && !strings.Contains(k, "manifest-") {
				out = append(out, k)
			}
		}
		sort.Strings(out)
		return out
	}
	gen(`["one"]`)
	gen(`["two"]`)
	if n := len(snapFiles()); n != 3 { // {} + one (previous generation) + two
		t.Fatalf("after 2 generations want 3 data files, got %v", snapFiles())
	}
	gen(`["three"]`)
	if n := len(snapFiles()); n != 3 { // one is gone now
		t.Fatalf("after 3 generations want 3 data files, got %v", snapFiles())
	}
	if len(f.dels) == 0 {
		t.Error("nothing was deleted")
	}
}

func TestDeployShrinkGuard(t *testing.T) {
	_, srv := newFakeStorage(t)
	big := map[string]string{}
	for i := 0; i < 20; i++ {
		big[fmt.Sprintf("/api/v2/detections/%d", 100+i)] = fmt.Sprintf(`{"id":%d}`, 100+i)
	}
	_, dir := buildTestSite(t, big)
	if _, err := deploy(context.Background(), deployConfig(srv, dir)); err != nil {
		t.Fatal(err)
	}
	_, small := buildTestSite(t, nil)
	c := deployConfig(srv, small)
	if _, err := deploy(context.Background(), c); err == nil || !strings.Contains(err.Error(), "not deploying") {
		t.Fatalf("shrink guard did not trip: %v", err)
	}
	c.Force = true
	if _, err := deploy(context.Background(), c); err != nil {
		t.Fatalf("-force should override: %v", err)
	}
}

func TestDeployDryRunAndAuth(t *testing.T) {
	f, srv := newFakeStorage(t)
	_, dir := buildTestSite(t, nil)
	c := deployConfig(srv, dir)
	c.DryRun = true
	res, err := deploy(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.puts) != 0 || res.Uploaded == 0 {
		t.Errorf("dry run wrote %v (planned %d)", f.puts, res.Uploaded)
	}
	c.DryRun, c.ZoneKey = false, "wrong"
	if _, err := deploy(context.Background(), c); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("want 401, got %v", err)
	}
	c.ZoneKey = ""
	if _, err := deploy(context.Background(), c); err == nil {
		t.Error("want error without a key")
	}
	if _, err := deploy(context.Background(), deployConfig(srv, t.TempDir())); err == nil || !strings.Contains(err.Error(), "snapshot.json") {
		t.Errorf("want missing snapshot.json error, got %v", err)
	}
}

// fakeAPI is an in-memory slice of the Bunny account API used by bunny-setup.
type fakeAPI struct {
	mu      sync.Mutex
	nextID  int64
	storage []map[string]any
	pulls   []map[string]any
	dns     []map[string]any
	calls   []string
	certOK  bool
}

func (a *fakeAPI) id() int64 { a.nextID++; return a.nextID }

func (a *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, r.Method+" "+r.URL.Path)
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	find := func(list []map[string]any, id string) map[string]any {
		for _, z := range list {
			if fmt.Sprint(z["Id"]) == id {
				return z
			}
		}
		return nil
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	switch {
	case r.URL.Path == "/storagezone" && r.Method == http.MethodGet:
		_ = enc.Encode(a.storage)
	case r.URL.Path == "/storagezone" && r.Method == http.MethodPost:
		z := map[string]any{"Id": a.id(), "Name": in["Name"], "Region": in["Region"], "Password": "pw-" + fmt.Sprint(in["Name"]),
			"StorageHostname": "la.storage.bunnycdn.com", "Rewrite404To200": false}
		a.storage = append(a.storage, z)
		_ = enc.Encode(z)
	case parts[0] == "storagezone" && len(parts) == 2:
		z := find(a.storage, parts[1])
		if z == nil {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost:
			for k, v := range in {
				z[k] = v
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			for i := range a.storage {
				if a.storage[i]["Id"] == z["Id"] {
					a.storage = append(a.storage[:i], a.storage[i+1:]...)
					break
				}
			}
		default:
			_ = enc.Encode(z)
		}
	case r.URL.Path == "/pullzone" && r.Method == http.MethodGet:
		_ = enc.Encode(a.pulls)
	case r.URL.Path == "/pullzone" && r.Method == http.MethodPost:
		z := map[string]any{"Id": a.id(), "Name": in["Name"], "StorageZoneId": in["StorageZoneId"],
			"Hostnames": []any{map[string]any{"Value": fmt.Sprint(in["Name"]) + ".b-cdn.net", "HasCertificate": true}}, "EdgeRules": []any{}}
		a.pulls = append(a.pulls, z)
		_ = enc.Encode(z)
	case r.URL.Path == "/pullzone/loadFreeCertificate":
		if !a.certOK {
			http.Error(w, `{"Message":"DNS not ready"}`, http.StatusBadRequest)
			return
		}
		for _, z := range a.pulls {
			for _, h := range z["Hostnames"].([]any) {
				hm := h.(map[string]any)
				if hm["Value"] == r.URL.Query().Get("hostname") {
					hm["HasCertificate"] = true
				}
			}
		}
	case parts[0] == "pullzone" && len(parts) >= 2:
		z := find(a.pulls, parts[1])
		if z == nil {
			http.NotFound(w, r)
			return
		}
		switch {
		case len(parts) == 2 && r.Method == http.MethodDelete:
			for i := range a.pulls {
				if a.pulls[i]["Id"] == z["Id"] {
					a.pulls = append(a.pulls[:i], a.pulls[i+1:]...)
					break
				}
			}
		case len(parts) == 2:
			for k, v := range in {
				z[k] = v
			}
		case parts[2] == "edgerules":
			z["EdgeRules"] = append(z["EdgeRules"].([]any), map[string]any{"Description": in["Description"],
				"ActionType": in["ActionType"], "ActionParameter1": in["ActionParameter1"], "Enabled": in["Enabled"], "Triggers": in["Triggers"]})
		case parts[2] == "addHostname":
			z["Hostnames"] = append(z["Hostnames"].([]any), map[string]any{"Value": in["Hostname"], "HasCertificate": false, "ForceSSL": false})
		case parts[2] == "setForceSSL":
			for _, h := range z["Hostnames"].([]any) {
				if hm := h.(map[string]any); hm["Value"] == in["Hostname"] {
					hm["ForceSSL"] = true
				}
			}
		}
	case r.URL.Path == "/dnszone":
		_ = enc.Encode(map[string]any{"Items": a.dns})
	case parts[0] == "dnszone" && len(parts) == 2:
		_ = enc.Encode(find(a.dns, parts[1]))
	case parts[0] == "dnszone" && len(parts) >= 3 && r.Method == http.MethodPut:
		z := find(a.dns, parts[1])
		z["Records"] = append(z["Records"].([]any), map[string]any{"Id": a.id(), "Type": in["Type"], "Name": in["Name"], "Value": in["Value"]})
	case parts[0] == "dnszone" && len(parts) == 4 && r.Method == http.MethodDelete:
		z := find(a.dns, parts[1])
		recs := z["Records"].([]any)
		for i, rec := range recs {
			if fmt.Sprint(rec.(map[string]any)["Id"]) == parts[3] {
				z["Records"] = append(recs[:i], recs[i+1:]...)
				break
			}
		}
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
	}
}

func (a *fakeAPI) count(prefix string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestBunnySetupIsIdempotent(t *testing.T) {
	fa := &fakeAPI{certOK: true, dns: []map[string]any{{"Id": int64(900), "Domain": "example.com", "Records": []any{}}}}
	srv := httptest.NewServer(fa)
	defer srv.Close()
	api := &bunnyAPI{base: srv.URL, key: "k", client: srv.Client()}
	o := setupOpts{Name: "birds-test", Region: "LA", Hostname: "birds.example.com", DNS: true, EdgeTTL: 60, CertTries: 1}
	envFile := filepath.Join(t.TempDir(), "env")
	o.EnvFile = envFile

	var out strings.Builder
	if err := bunnySetup(context.Background(), api, o, &out); err != nil {
		t.Fatal(err)
	}
	if fa.count("POST /storagezone") < 1 || fa.count("POST /pullzone") < 1 {
		t.Fatalf("zones not created: %v", fa.calls)
	}
	if !strings.Contains(out.String(), "https://birds.example.com") {
		t.Errorf("want custom URL in output: %s", out.String())
	}
	env, _ := os.ReadFile(envFile)
	if !strings.Contains(string(env), "BUNNY_STORAGE_KEY=pw-birds-test") {
		t.Errorf("env file: %s", env)
	}
	if fi, _ := os.Stat(envFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("env file mode %v", fi.Mode().Perm())
	}
	recs := fa.dns[0]["Records"].([]any)
	if len(recs) != 1 || recs[0].(map[string]any)["Value"] != "birds-test.b-cdn.net" {
		t.Errorf("CNAME not created: %v", recs)
	}

	// Second run: nothing new is created.
	creates := fa.count("POST /storagezone") + fa.count("POST /pullzone/") + fa.count("PUT /dnszone")
	before := len(fa.storage) + len(fa.pulls)
	out.Reset()
	if err := bunnySetup(context.Background(), api, o, &out); err != nil {
		t.Fatal(err)
	}
	if len(fa.storage)+len(fa.pulls) != before || len(fa.dns[0]["Records"].([]any)) != 1 {
		t.Error("second run duplicated resources")
	}
	if fa.count("POST /pullzone/2/edgerules") != 2 || fa.count("POST /pullzone/2/addHostname") != 1 || fa.count("PUT /dnszone") != 1 {
		t.Errorf("second run repeated one-time steps: %v", fa.calls)
	}
	_ = creates

	// Teardown needs -yes, then removes everything.
	if err := bunnyTeardown(context.Background(), api, o); err == nil {
		t.Error("teardown without -yes must refuse")
	}
	o.Yes = true
	if err := bunnyTeardown(context.Background(), api, o); err != nil {
		t.Fatal(err)
	}
	if len(fa.storage) != 0 || len(fa.pulls) != 0 || len(fa.dns[0]["Records"].([]any)) != 0 {
		t.Errorf("teardown left %v %v %v", fa.storage, fa.pulls, fa.dns)
	}
}

func TestBunnySetupWithoutCertOrDNS(t *testing.T) {
	fa := &fakeAPI{certOK: false}
	srv := httptest.NewServer(fa)
	defer srv.Close()
	api := &bunnyAPI{base: srv.URL, key: "k", client: srv.Client()}
	var out strings.Builder
	o := setupOpts{Name: "nodns", Region: "DE", Hostname: "birds.elsewhere.org", DNS: true, CertTries: 1}
	if err := bunnySetup(context.Background(), api, o, &out); err != nil {
		t.Fatal(err)
	}
	// No certificate yet: fall back to the b-cdn.net URL and never force SSL.
	if !strings.Contains(out.String(), "https://nodns.b-cdn.net") || fa.count("POST /pullzone/2/setForceSSL") != 0 {
		t.Errorf("output %s calls %v", out.String(), fa.calls)
	}
	if !strings.Contains(out.String(), "BUNNY_STORAGE_KEY=pw-nodns") {
		t.Errorf("settings not printed: %s", out.String())
	}
}

func TestEnsureCNAMERefusesToClobber(t *testing.T) {
	fa := &fakeAPI{dns: []map[string]any{{"Id": int64(1), "Domain": "example.com",
		"Records": []any{map[string]any{"Id": 5, "Type": 0, "Name": "birds", "Value": "1.2.3.4"}}}}}
	srv := httptest.NewServer(fa)
	defer srv.Close()
	api := &bunnyAPI{base: srv.URL, key: "k", client: srv.Client()}
	if err := ensureCNAME(context.Background(), api, "birds.example.com", "x.b-cdn.net"); err == nil || !strings.Contains(err.Error(), "not touching") {
		t.Errorf("want refusal, got %v", err)
	}
	if err := ensureCNAME(context.Background(), api, "example.com", "x.b-cdn.net"); err == nil || !strings.Contains(err.Error(), "apex") {
		t.Errorf("want apex refusal, got %v", err)
	}
}
