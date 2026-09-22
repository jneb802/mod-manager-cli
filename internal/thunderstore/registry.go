package thunderstore

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const hexiumBaseURL = "https://valheim.hexium.gg"

// Registry is a Thunderstore-compatible package registry.
//
// Hexium serves the same experimental API shape as Thunderstore, so both are
// queried through the same code path. Several Valheim mod authors publish
// their newest builds only on Hexium, which a Thunderstore-only lookup reports
// as a missing package or a missing version.
type Registry struct {
	// Name identifies the registry in errors and user-facing output.
	Name string
	// BaseURL is the registry root, without a trailing slash.
	BaseURL string
	// URLHosts are the hosts whose package URLs belong to this registry.
	URLHosts []string
	// URLMarker is the path segment directly before /{owner}/{name} in a
	// package URL: thunderstore.io/c/valheim/p/Owner/Name and
	// valheim.hexium.gg/mods/Owner/Name.
	URLMarker string
}

// registries are queried in this order. Thunderstore stays first so that it
// wins ties and remains the source for everything it already served.
var registries = []Registry{
	{
		Name:      "Thunderstore",
		BaseURL:   baseURL,
		URLHosts:  []string{"thunderstore.io"},
		URLMarker: "p",
	},
	{
		Name:      "Hexium",
		BaseURL:   hexiumBaseURL,
		URLHosts:  []string{"valheim.hexium.gg", "hexium.gg"},
		URLMarker: "mods",
	},
}

// Registries returns the configured registries in preference order.
func Registries() []Registry {
	return registries
}

func (r Registry) experimentalAPI() string {
	return r.BaseURL + "/api/experimental/package/"
}

// registryForURL returns the registry a package URL belongs to.
func registryForURL(rawURL string) (Registry, bool) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	if trimmed == rawURL {
		return Registry{}, false
	}
	host := trimmed
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	for _, r := range registries {
		for _, h := range r.URLHosts {
			if host == h {
				return r, true
			}
		}
	}
	return Registry{}, false
}

// ownerNameFromURL extracts the owner and name from a package URL belonging to
// r. It reports false when the URL does not carry both segments.
func ownerNameFromURL(r Registry, rawURL string) (owner, name string, ok bool) {
	parts := strings.Split(strings.Trim(rawURL, "/"), "/")
	for i, p := range parts {
		if p == r.URLMarker && i+2 < len(parts) {
			return parts[i+1], parts[i+2], true
		}
	}
	return "", "", false
}

// latestVersionNumber returns the version number a lookup resolved to.
func latestVersionNumber(pkg *Package) string {
	if pkg == nil || len(pkg.Versions) == 0 {
		return ""
	}
	return pkg.Versions[0].VersionNumber
}

// compareVersions compares two dotted numeric version strings. It returns a
// negative value when a sorts before b, zero when they are equal and a
// positive value when a sorts after b. Segments that are missing or not
// numeric count as zero, matching the numeric triples both registries publish.
func compareVersions(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		if d := versionSegment(as, i) - versionSegment(bs, i); d != 0 {
			return d
		}
	}
	return 0
}

func versionSegment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(parts[i]))
	if err != nil {
		return 0
	}
	return v
}

// rateLimitAttempts is how many times a registry request is retried after the
// registry answers 429. A bulk operation such as a profile import issues one
// request per mod per registry, which is enough to be rate limited part way
// through; without a retry those mods are reported as missing packages.
const rateLimitAttempts = 4

// rateLimitBackoff is the wait before each retry.
var rateLimitBackoff = []time.Duration{time.Second, 3 * time.Second, 6 * time.Second, 12 * time.Second}

// errRateLimited reports that a registry rate limited the request and kept
// doing so for every attempt.
type errRateLimited struct {
	retries int
}

func (e *errRateLimited) Error() string {
	return fmt.Sprintf("rate limited (HTTP 429) after %d attempts — wait a moment and try again", e.retries+1)
}

// getWithRetry issues a GET and retries while the registry answers 429.
// The caller owns closing the response body.
func getWithRetry(url string) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; ; attempt++ {
		resp, err = http.Get(url)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		resp.Body.Close()
		if attempt >= rateLimitAttempts-1 {
			return nil, &errRateLimited{retries: attempt}
		}

		wait := rateLimitBackoff[len(rateLimitBackoff)-1]
		if attempt < len(rateLimitBackoff) {
			wait = rateLimitBackoff[attempt]
		}
		if after := retryAfter(resp); after > 0 {
			wait = after
		}
		time.Sleep(wait)
	}
}

// retryAfter reads a Retry-After header expressed in seconds. Values that are
// absent, unparseable or implausibly long are ignored in favour of the backoff.
func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || secs <= 0 || secs > 30 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
