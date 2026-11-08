package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const actionSource = "name: test\ndescription: test\nruns: {using: composite, steps: []}\n"

var sha = strings.Repeat("a", 40)

func ref(t *testing.T, spec string) actions.Reference {
	t.Helper()
	r, e := actions.ParseReference("github.com", spec)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestDefaultOnlineImmutableMetadataAndWarmOffline(t *testing.T) {
	requests := 0
	cache := t.TempDir()
	client, err := New(Options{CacheDir: cache, Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "api.github.com" {
			t.Fatalf("foreign host %s", r.URL.Host)
		}
		body := ""
		switch r.URL.Path {
		case "/repos/example/action":
			body = `{"id":42,"full_name":"example/action"}`
		case "/repos/example/action/git/commits/" + sha:
			body = `{"sha":"` + sha + `"}`
		case "/repos/example/action/contents/sub/action.yml":
			if r.URL.Query().Get("ref") != sha {
				t.Fatal("manifest not pinned")
			}
			body = actionSource
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action/sub@"+sha)
	manifest, err := client.Lookup(t.Context(), request)
	if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().RepositoryID() != 42 || requests != 4 {
		t.Fatalf("manifest=%v error=%v requests=%d", manifest, err, requests)
	}
	offline, err := New(Options{Mode: Offline, CacheDir: cache, Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline network")
		return nil, errors.New("network")
	})})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := offline.Lookup(t.Context(), request)
	if err != nil || !warm.Available() || warm.Content() != actionSource {
		t.Fatalf("warm=%v error=%v", warm, err)
	}
	miss, err := offline.Lookup(t.Context(), ref(t, "other/action/sub@"+sha))
	if err != nil || miss.Available() || miss.Reason() != availability.CacheMissReason {
		t.Fatalf("foreign repo cache=%v error=%v", miss, err)
	}
}

func TestNativeHostDoesNotResolveNamesOrSHA256(t *testing.T) {
	client, err := New(Options{Mode: Online, Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported request used network")
		return nil, errors.New("network")
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"example/action@v4", "example/action@" + strings.Repeat("a", 64)} {
		m, err := client.Lookup(t.Context(), ref(t, spec))
		if err != nil || m.Reason() != availability.UnsupportedReason {
			t.Fatalf("%s = %v, %v", spec, m, err)
		}
	}
}

func TestTransportFailuresNeverLeakCredentials(t *testing.T) {
	for _, status := range []int{302, 403, 404, 429} {
		client, err := New(Options{Mode: Online, Token: "secret-token", Transport: transport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://foreign.test/?token=secret-token"}}, Body: io.NopCloser(strings.NewReader("secret-token")), Request: r}, nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		if err != nil || m.Available() || strings.Contains(string(m.Reason()), "secret-token") {
			t.Fatalf("unsafe failure %v, %v", m, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	client, err := New(Options{Mode: Online, Transport: transport(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lookup(ctx, ref(t, "example/action@"+sha)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
