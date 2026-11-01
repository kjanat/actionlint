package oci

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/acquisitionhttp"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type acquisitionResponse func(*http.Request) (*http.Response, error)

func (f acquisitionResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestAutomaticOCIInitialAndPinNeverDialPrivate(t *testing.T) {
	previous := http.DefaultTransport
	dials := 0
	http.DefaultTransport = &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("unexpected private connection")
	}}
	t.Cleanup(func() { http.DefaultTransport = previous })
	source := "oci://127.0.0.1:8443/actionlint/provider@sha256:" + strings.Repeat("a", 64)
	if _, err := artifact.Resolve(t.Context(), source, artifact.Options{PullOCI: Pull}); !errors.Is(err, acquisitionhttp.PrivateDestination) {
		t.Fatalf("private automatic OCI pull: %v", err)
	}
	if _, err := Pin(t.Context(), "oci://127.0.0.1:8443/actionlint/provider:v1", artifact.Options{}); !errors.Is(err, acquisitionhttp.PrivateDestination) {
		t.Fatalf("private automatic OCI tag resolution: %v", err)
	}
	if dials != 0 {
		t.Fatalf("private OCI acquisition dialed %d times", dials)
	}
}

func TestOCIGrantCannotAuthorizeTokenRealmOrBlobRedirectAuthority(t *testing.T) {
	const initial = "oci://127.0.0.1:8443/actionlint/provider@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref, err := artifact.ParseOCI(initial)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := artifact.ExplicitSource(initial)
	if err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	for _, mode := range []string{"token", "blob"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			http.DefaultTransport = acquisitionResponse(func(request *http.Request) (*http.Response, error) {
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
			repo, err := repository(ref, artifact.Options{Network: grant})
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
			if !errors.Is(err, acquisitionhttp.UnsupportedTransport) || calls != 1 {
				t.Fatalf("OCI %s escaped initial grant: %v calls=%d", mode, err, calls)
			}
		})
	}
}
