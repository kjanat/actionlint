package artifacts

import (
	"net/netip"
	"net/url"
	"strings"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

// ExplicitSource grants the initial HTTP authority named by an invocation or
// public API argument. Configuration, locks and caches must not call it.
// Local sources need no network grant. OCI tags are accepted for explicit pinning.
func ExplicitSource(source string) (acquisitionhttp.Grant, error) {
	if IsOCI(source) {
		ref, err := ParseOCITag(source)
		if err != nil {
			return acquisitionhttp.Grant{}, err
		}
		return acquisitionhttp.ExplicitEndpoint(ref.Endpoint())
	}
	endpoint, local, _, err := parseSource(source)
	if err != nil || local != "" || endpoint == nil || endpoint.Scheme == "sha256" {
		return acquisitionhttp.Grant{}, err
	}
	return acquisitionhttp.ExplicitEndpoint(endpoint)
}

// Endpoint returns the HTTP authority used by the OCI client. Loopback sources
// use plain HTTP like Docker; public-only policy still rejects their addresses.
func (r OCIReference) Endpoint() *url.URL {
	endpoint := &url.URL{Scheme: "https", Host: r.host}
	host := endpoint.Hostname()
	ip, err := netip.ParseAddr(host)
	if strings.EqualFold(host, "localhost") || err == nil && ip.IsLoopback() {
		endpoint.Scheme = "http"
	}
	return endpoint
}
