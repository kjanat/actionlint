package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const actionSource = "name: test\ndescription: test\nruns: {using: composite, steps: []}\n"

var sha = strings.Repeat("a", 40)

func knownRepositoryID(t *testing.T, value uint64) repositoryid.ID {
	t.Helper()
	id, err := repositoryid.Known(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ref(t *testing.T, spec string) actions.Reference {
	t.Helper()
	r, e := actions.ParseReference("github.com", spec)
	if e != nil {
		t.Fatal(e)
	}
	return r
}

func TestDefaultOnlineImmutableMetadataAndWarmOffline(t *testing.T) {
	cache := t.TempDir()
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "raw.githubusercontent.com" || r.URL.Path != "/example/action/"+sha+"/sub/action.yml" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		return respond(r, http.StatusOK, rawText, actionSource), nil
	}}
	client, err := New(Options{CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action/sub@"+sha)
	manifest, err := client.Lookup(t.Context(), request)
	if err != nil || !manifest.Available() || manifest.Content() != actionSource || !manifest.Identity().RepositoryID().IsZero() || len(hosts.recorded()) != 1 {
		t.Fatalf("manifest=%v error=%v requests=%v", manifest, err, hosts.recorded())
	}
	again, err := New(Options{CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := again.Lookup(t.Context(), request); err != nil || !hit.Available() || hit.Content() != actionSource || len(hosts.recorded()) != 1 {
		t.Fatalf("warm online=%v error=%v requests=%v", hit, err, hosts.recorded())
	}
	offline, err := New(Options{Mode: Offline, CacheDir: cache, Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline network")
		return nil, errors.New("network")
	})})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := offline.Lookup(t.Context(), request)
	if err != nil || !warm.Available() || warm.Content() != actionSource || !warm.Identity().RepositoryID().IsZero() {
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
	for _, tc := range []struct {
		spec string
		want availability.Reason
	}{
		{"example/action@v4", availability.ResolverNotConfiguredReason},
		{"example/action@" + strings.Repeat("a", 64), availability.UnsupportedObjectFormatReason},
	} {
		m, err := client.Lookup(t.Context(), ref(t, tc.spec))
		if err != nil || m.Reason() != tc.want {
			t.Fatalf("%s = %v, %v; want %s", tc.spec, m, err, tc.want)
		}
	}
}

func TestUnsupportedMetadataHostDoesNotUseNetwork(t *testing.T) {
	client, err := New(Options{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsupported host used the GitHub API")
		return nil, errors.New("unexpected network request")
	})})
	if err != nil {
		t.Fatal(err)
	}
	reference, err := actions.ParseReference("codeberg.org", "example/action@v1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Lookup(t.Context(), reference)
	if err != nil || result.Reason() != availability.UnsupportedHostReason {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestTransportFailuresNeverLeakCredentials(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   availability.Reason
		api    bool
	}{
		{http.StatusFound, availability.FailedReason, false},
		{http.StatusNotFound, availability.InaccessibleReason, false},
		{http.StatusForbidden, availability.DeniedReason, true},
		{http.StatusTooManyRequests, availability.LimitReason, true},
	} {
		hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host {
			case "raw.githubusercontent.com", "api.github.com":
			case "cdn.jsdelivr.net":
				if r.Header.Get("Authorization") != "" || strings.Contains(r.URL.String(), "secret-token") {
					t.Fatalf("credentials sent to jsDelivr: %s", r.URL)
				}
			default:
				t.Fatalf("foreign authority %s", r.URL)
			}
			response := respond(r, tc.status, "text/html", "secret-token")
			response.Header.Set("Location", "https://foreign.test/?token=secret-token")
			return response, nil
		}}
		client, err := New(Options{Mode: Online, Token: "secret-token", Transport: hosts, Mirrors: references.AllowUnknownMirrors})
		if err != nil {
			t.Fatal(err)
		}
		m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		if err != nil || m.Available() || m.Reason() != tc.want || strings.Contains(string(m.Reason()), "secret-token") || (hosts.count("api.github.com") != 0) != tc.api || (hosts.count("cdn.jsdelivr.net") != 0) != tc.api {
			t.Fatalf("status %d: unsafe failure %v, %v requests=%v", tc.status, m, err, hosts.recorded())
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
