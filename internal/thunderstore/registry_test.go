package thunderstore

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int // sign of the expected result
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", want: 0},
		{name: "patch newer", a: "3.1.6", b: "3.1.4", want: 1},
		{name: "patch older", a: "1.0.6", b: "1.0.7", want: -1},
		{name: "minor beats patch", a: "1.2.0", b: "1.1.99", want: 1},
		{name: "major beats minor", a: "2.0.0", b: "1.99.99", want: 1},
		{name: "multi digit segments", a: "1.8.22", b: "1.8.9", want: 1},
		{name: "missing segments count as zero", a: "1.2", b: "1.2.0", want: 0},
		{name: "non numeric counts as zero", a: "1.2.x", b: "1.2.0", want: 0},
		{name: "empty is lowest", a: "", b: "0.0.1", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if sign(got) != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
			}
			if sign(compareVersions(tt.b, tt.a)) != -tt.want {
				t.Errorf("compareVersions(%q, %q) is not symmetric", tt.b, tt.a)
			}
		})
	}
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	default:
		return 0
	}
}

func TestRegistryForURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantOK   bool
	}{
		{name: "thunderstore", input: "https://thunderstore.io/c/valheim/p/Azumatt/AzuAutoStore/", wantName: "Thunderstore", wantOK: true},
		{name: "thunderstore http", input: "http://thunderstore.io/c/valheim/p/Azumatt/AzuAutoStore/", wantName: "Thunderstore", wantOK: true},
		{name: "hexium", input: "https://valheim.hexium.gg/mods/Azumatt/AzuAutoStore", wantName: "Hexium", wantOK: true},
		{name: "hexium apex domain", input: "https://hexium.gg/mods/Azumatt/AzuAutoStore", wantName: "Hexium", wantOK: true},
		{name: "unknown host", input: "https://example.com/mods/Azumatt/AzuAutoStore", wantOK: false},
		{name: "owner-name is not a url", input: "Azumatt-AzuAutoStore", wantOK: false},
		{name: "host prefix must not match a substring", input: "https://nothexium.gg/mods/A/B", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := registryForURL(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("registryForURL(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && r.Name != tt.wantName {
				t.Errorf("registryForURL(%q) = %q, want %q", tt.input, r.Name, tt.wantName)
			}
		})
	}
}

func TestOwnerNameFromURL(t *testing.T) {
	thunderstore, hexium := registries[0], registries[1]

	tests := []struct {
		name      string
		registry  Registry
		input     string
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{name: "thunderstore", registry: thunderstore, input: "https://thunderstore.io/c/valheim/p/Azumatt/AzuAutoStore/", wantOwner: "Azumatt", wantName: "AzuAutoStore", wantOK: true},
		{name: "hexium", registry: hexium, input: "https://valheim.hexium.gg/mods/Smoothbrain/TargetPortal", wantOwner: "Smoothbrain", wantName: "TargetPortal", wantOK: true},
		{name: "missing name segment", registry: hexium, input: "https://valheim.hexium.gg/mods/Smoothbrain", wantOK: false},
		{name: "marker absent", registry: hexium, input: "https://valheim.hexium.gg/about", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, name, ok := ownerNameFromURL(tt.registry, tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ownerNameFromURL(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && (owner != tt.wantOwner || name != tt.wantName) {
				t.Errorf("ownerNameFromURL(%q) = %q/%q, want %q/%q", tt.input, owner, name, tt.wantOwner, tt.wantName)
			}
		})
	}
}

// fakeRegistry serves the experimental package API for a single package.
// A version of "" means the registry does not carry the package at all.
func fakeRegistry(t *testing.T, name, latest string, versions ...string) (Registry, *httptest.Server) {
	t.Helper()

	has := make(map[string]bool, len(versions))
	for _, v := range versions {
		has[v] = true
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// /api/experimental/package/{owner}/{name}[/{version}]
		switch len(parts) {
		case 5:
			if latest == "" {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"namespace":%q,"name":%q,"full_name":"%s-%s","latest":{"version_number":%q,"download_url":"%s/dl/%s.zip"}}`,
				parts[3], parts[4], parts[3], parts[4], latest, "http://"+r.Host, latest)
		case 6:
			if !has[parts[5]] {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"version_number":%q,"download_url":"%s/dl/%s.zip"}`, parts[5], "http://"+r.Host, parts[5])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return Registry{Name: name, BaseURL: srv.URL, URLHosts: []string{"invalid.test"}, URLMarker: "p"}, srv
}

// useRegistries swaps the package-level registry list for the duration of a test.
func useRegistries(t *testing.T, rs ...Registry) {
	t.Helper()
	original := registries
	registries = rs
	t.Cleanup(func() { registries = original })
}

func TestGetPackageVersionFallsBackToNextRegistry(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "1.0.4", "1.0.4")
	second, _ := fakeRegistry(t, "Second", "1.0.5", "1.0.4", "1.0.5")
	useRegistries(t, first, second)

	pkg, err := GetPackageVersion("Azumatt", "TrueInstantLootDrop", "1.0.5")
	if err != nil {
		t.Fatalf("GetPackageVersion: %v", err)
	}
	if got := pkg.Versions[0].VersionNumber; got != "1.0.5" {
		t.Errorf("version = %q, want 1.0.5", got)
	}
	if pkg.Source != "Second" {
		t.Errorf("Source = %q, want Second", pkg.Source)
	}
}

func TestGetPackageVersionPrefersFirstRegistry(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "1.0.4", "1.0.4")
	second, _ := fakeRegistry(t, "Second", "1.0.4", "1.0.4")
	useRegistries(t, first, second)

	pkg, err := GetPackageVersion("Azumatt", "TrueInstantLootDrop", "1.0.4")
	if err != nil {
		t.Fatalf("GetPackageVersion: %v", err)
	}
	if pkg.Source != "First" {
		t.Errorf("Source = %q, want First", pkg.Source)
	}
}

func TestGetPackageVersionErrorNamesEveryRegistry(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "1.0.4", "1.0.4")
	second, _ := fakeRegistry(t, "Second", "1.0.4", "1.0.4")
	useRegistries(t, first, second)

	_, err := GetPackageVersion("Azumatt", "TrueInstantLootDrop", "9.9.9")
	if err == nil {
		t.Fatal("expected an error for a version no registry carries")
	}
	for _, want := range []string{"First", "Second", "9.9.9"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestGetPackagePrefersHighestVersionAcrossRegistries(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "3.1.4", "3.1.4")
	second, _ := fakeRegistry(t, "Second", "3.1.6", "3.1.6")
	useRegistries(t, first, second)

	pkg, err := GetPackage("Azumatt", "AzuAutoStore")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got := pkg.Versions[0].VersionNumber; got != "3.1.6" {
		t.Errorf("version = %q, want 3.1.6", got)
	}
	if pkg.Source != "Second" {
		t.Errorf("Source = %q, want Second", pkg.Source)
	}
}

func TestGetPackageTiePrefersFirstRegistry(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "3.1.6", "3.1.6")
	second, _ := fakeRegistry(t, "Second", "3.1.6", "3.1.6")
	useRegistries(t, first, second)

	pkg, err := GetPackage("Azumatt", "AzuAutoStore")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if pkg.Source != "First" {
		t.Errorf("Source = %q, want First", pkg.Source)
	}
}

func TestGetPackageResolvesWhenOnlyOneRegistryHasIt(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "")
	second, _ := fakeRegistry(t, "Second", "1.1.8", "1.1.8")
	useRegistries(t, first, second)

	pkg, err := GetPackage("Smoothbrain", "PassivePowers")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if got := pkg.Versions[0].VersionNumber; got != "1.1.8" {
		t.Errorf("version = %q, want 1.1.8", got)
	}
}

func TestGetPackageErrorNamesEveryRegistry(t *testing.T) {
	first, _ := fakeRegistry(t, "First", "")
	second, _ := fakeRegistry(t, "Second", "")
	useRegistries(t, first, second)

	_, err := GetPackage("Nobody", "Nothing")
	if err == nil {
		t.Fatal("expected an error when no registry carries the package")
	}
	for _, want := range []string{"First", "Second", "Nobody-Nothing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestDefaultRegistriesAreThunderstoreThenHexium(t *testing.T) {
	got := Registries()
	if len(got) != 2 {
		t.Fatalf("Registries() returned %d registries, want 2", len(got))
	}
	if got[0].Name != "Thunderstore" || got[0].BaseURL != "https://thunderstore.io" {
		t.Errorf("first registry = %+v, want Thunderstore", got[0])
	}
	if got[1].Name != "Hexium" || got[1].BaseURL != "https://valheim.hexium.gg" {
		t.Errorf("second registry = %+v, want Hexium", got[1])
	}
	if got[0].experimentalAPI() != "https://thunderstore.io/api/experimental/package/" {
		t.Errorf("Thunderstore experimental API = %q", got[0].experimentalAPI())
	}
}

// shortenBackoff keeps rate-limit tests fast.
func shortenBackoff(t *testing.T) {
	t.Helper()
	original := rateLimitBackoff
	rateLimitBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { rateLimitBackoff = original })
}

func TestGetWithRetryRetriesRateLimits(t *testing.T) {
	shortenBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, `{"version_number":"1.0.0"}`)
	}))
	defer srv.Close()

	resp, err := getWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("getWithRetry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("made %d requests, want 3", got)
	}
}

func TestGetWithRetryGivesUpOnPersistentRateLimit(t *testing.T) {
	shortenBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := getWithRetry(srv.URL)
	if err == nil {
		t.Fatal("expected an error when every attempt is rate limited")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error %q should name the rate limit", err)
	}
	if got := atomic.LoadInt32(&calls); got != rateLimitAttempts {
		t.Errorf("made %d requests, want %d", got, rateLimitAttempts)
	}
}

func TestGetWithRetryDoesNotRetryOtherStatuses(t *testing.T) {
	shortenBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	resp, err := getWithRetry(srv.URL)
	if err != nil {
		t.Fatalf("getWithRetry: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("made %d requests, want 1 — a 404 is an answer, not a retry", got)
	}
}

func TestRateLimitedLookupIsReportedAsRateLimited(t *testing.T) {
	shortenBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	limited := Registry{Name: "Limited", BaseURL: srv.URL, URLMarker: "p"}
	useRegistries(t, limited)

	_, err := GetPackageVersion("Searica", "DodgeShortcut", "1.4.0")
	if err == nil {
		t.Fatal("expected an error")
	}
	// A rate limited mod must not look like a mod that does not exist.
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error %q should say the request was rate limited", err)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{name: "seconds", header: "5", want: 5 * time.Second},
		{name: "absent", header: "", want: 0},
		{name: "http date is ignored", header: "Wed, 21 Oct 2026 07:28:00 GMT", want: 0},
		{name: "zero ignored", header: "0", want: 0},
		{name: "implausibly long ignored", header: "600", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tt.header != "" {
				resp.Header.Set("Retry-After", tt.header)
			}
			if got := retryAfter(resp); got != tt.want {
				t.Errorf("retryAfter(%q) = %v, want %v", tt.header, got, tt.want)
			}
		})
	}
}
