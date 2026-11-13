package oci

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	"actionlint.kjanat.dev/v2/internal/plugins/artifacts"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type acquisitionResponse func(*http.Request) (*http.Response, error)

func (f acquisitionResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAutomaticOCIInitialAndPinNeverDialPrivate(t *testing.T) {
	t.Parallel()
	dials := 0
	connections := acquisitionhttp.Connections{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected private connection")
	}}}
	source := "oci://127.0.0.1:8443/actionlint/provider@sha256:" + strings.Repeat("a", 64)
	if _, err := artifacts.Resolve(t.Context(), source, artifacts.Options{PullOCI: Pull, Connections: connections}); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) {
		t.Fatalf("private automatic OCI pull: %v", err)
	}
	if _, err := Pin(t.Context(), "oci://127.0.0.1:8443/actionlint/provider:v1", artifacts.Options{Connections: connections}); !errors.Is(err, acquisitionhttp.ErrPrivateDestination) {
		t.Fatalf("private automatic OCI tag resolution: %v", err)
	}
	if dials != 0 {
		t.Fatalf("private OCI acquisition dialed %d times", dials)
	}
}

func TestOCIGrantCannotAuthorizeTokenRealmOrBlobRedirectAuthority(t *testing.T) {
	t.Parallel()
	const initial = "oci://127.0.0.1:8443/actionlint/provider@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref, err := artifacts.ParseOCI(initial)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := artifacts.ExplicitSource(initial)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"token", "blob"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			calls := 0
			transport := acquisitionResponse(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Host != "127.0.0.1:8443" {
					t.Fatal("server-selected authority reached opaque transport")
				}
				header := http.Header{}
				status := http.StatusTemporaryRedirect
				if mode == "token" {
					status = http.StatusUnauthorized
					header.Set("WWW-Authenticate", `Bearer realm="http://127.0.0.1:9443/token",service="fixture",scope="repository:actionlint/provider:pull"`)
				} else {
					header.Set("Location", "http://127.0.0.1:9443/blob")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
			})
			repo, err := repository(ref, artifacts.Options{Network: grant, Connections: acquisitionhttp.Connections{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "token" {
				_, err = repo.Resolve(t.Context(), "v1")
			} else {
				body, fetchErr := repo.Blobs().Fetch(t.Context(), ocispec.Descriptor{Digest: digest.Digest("sha256:" + strings.Repeat("a", 64))})
				if body != nil {
					body.Close()
				}
				err = fetchErr
			}
			if !errors.Is(err, acquisitionhttp.ErrUnsupportedTransport) || calls != 1 {
				t.Fatalf("OCI %s escaped initial grant: %v calls=%d", mode, err, calls)
			}
		})
	}
}
