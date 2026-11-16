package artifacts

import (
	"actionlint.kjanat.dev/v2/internal/integrations/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/model/pluginreference"
)

// ExplicitSource grants the initial HTTP authority named by an invocation or
// public API argument. Configuration, locks and caches must not call it.
// Local sources need no network grant. OCI tags are accepted for explicit pinning.
func ExplicitSource(source string) (acquisitionhttp.Grant, error) {
	if pluginreference.IsOCI(source) {
		ref, err := pluginreference.ParseOCITag(source)
		if err != nil {
			return acquisitionhttp.Grant{}, err
		}
		return acquisitionhttp.ExplicitEndpoint(ref.Endpoint())
	}
	location, err := pluginreference.ParseModuleLocation(source)
	if err != nil || location.Local != "" || location.Endpoint == nil || location.Endpoint.Scheme == "sha256" {
		return acquisitionhttp.Grant{}, err
	}
	return acquisitionhttp.ExplicitEndpoint(location.Endpoint)
}
