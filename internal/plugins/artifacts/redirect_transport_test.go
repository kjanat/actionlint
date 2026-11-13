package artifacts

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
)

type downloadResponse func(*http.Request) (*http.Response, error)

func (f downloadResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestDownloadAutomaticPrivateInitialIsRejected(t *testing.T) {
	t.Parallel()
	endpoint, err := url.Parse("https://127.0.0.1:8443/plugin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := download(t.Context(), endpoint, "", Options{Connections: acquisitionhttp.Connections{Transport: &http.Transport{}}}.withDefaults()); !errors.Is(err, errRedirectAddress) {
		t.Fatalf("automatic private acquisition: %v", err)
	}
}

func TestDownloadExplicitGrantPreservesInitialAndConfinesRedirect(t *testing.T) {
	t.Parallel()
	const initial = "https://127.0.0.1:8443/plugin?signature=secret"
	grant, err := ExplicitSource(initial)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(initial)
	if err != nil {
		t.Fatal(err)
	}
	for _, redirect := range []string{"", "https://127.0.0.1:9443/plugin", "https://other.example/plugin"} {
		calls := 0
		transport := downloadResponse(func(request *http.Request) (*http.Response, error) {
			calls++
			if request.URL.String() != initial {
				t.Fatal("opaque transport received ungranted URL")
			}
			status := http.StatusOK
			if redirect != "" {
				status = http.StatusFound
			}
			return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{redirect}}, Body: io.NopCloser(strings.NewReader("plugin")), Request: request}, nil
		})
		data, err := download(t.Context(), endpoint, "", Options{Network: grant, Connections: acquisitionhttp.Connections{Transport: transport}}.withDefaults())
		if redirect == "" {
			if err != nil || string(data) != "plugin" || calls != 1 {
				t.Fatalf("explicit private acquisition: %q %v calls=%d", data, err, calls)
			}
		} else if !errors.Is(err, errRedirectTransport) || calls != 1 {
			t.Fatalf("private redirect escaped grant: %v calls=%d", err, calls)
		}
	}
}

func TestAcquisitionErrorClassifications(t *testing.T) {
	t.Parallel()
	for failure, expected := range map[acquisitionhttp.PolicyError]error{
		acquisitionhttp.ErrInvalidEndpoint:      acquisitionhttp.ErrInvalidEndpoint,
		acquisitionhttp.ErrConnectionFailed:     acquisitionhttp.ErrConnectionFailed,
		acquisitionhttp.ErrPrivateDestination:   errRedirectAddress,
		acquisitionhttp.ErrUnsupportedTransport: errRedirectTransport,
		acquisitionhttp.ErrUnsupportedProxy:     errRedirectProxy,
		acquisitionhttp.ErrDNSFailed:            errRedirectDNS,
	} {
		wrapped := &url.Error{Op: "Get", URL: "https://example.org/?signed=secret", Err: failure}
		if err := acquisitionError(wrapped); !errors.Is(err, expected) || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe acquisition diagnostic: %v", err)
		}
	}
}
