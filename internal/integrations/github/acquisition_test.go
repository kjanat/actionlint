package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

var (
	tagObject = strings.Repeat("b", 40)
	branchTip = strings.Repeat("c", 40)
)

func actionListing() string {
	return listing(branchTip,
		gitwire.Ref{Name: "refs/heads/main", Object: branchTip},
		gitwire.Ref{Name: "refs/heads/release", Object: branchTip},
		gitwire.Ref{Name: "refs/tags/v1", Object: tagObject, Peeled: sha},
		gitwire.Ref{Name: "refs/tags/v2", Object: sha},
	)
}

func gitAndRawHosts(t *testing.T, files map[string]string) *recorder {
	t.Helper()
	return &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "github.com":
			if r.URL.Path != "/example/action.git/info/refs" || r.URL.RawQuery != "service=git-upload-pack" || r.Header.Get("Git-Protocol") != "" {
				t.Fatalf("unexpected listing request %s", r.URL)
			}
			return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
		case "raw.githubusercontent.com":
			if content, ok := files[r.URL.Path]; ok {
				return respond(r, http.StatusOK, rawText, content), nil
			}
			return respond(r, http.StatusNotFound, rawText, "404: Not Found"), nil
		}
		t.Fatalf("unexpected authority %s", r.URL)
		return nil, nil
	}}
}

func TestFullSHACacheHitMakesNoRequests(t *testing.T) {
	cache := t.TempDir()
	hosts := gitAndRawHosts(t, map[string]string{"/example/action/" + sha + "/action.yml": actionSource})
	cold, err := New(Options{CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	if manifest, err := cold.Lookup(t.Context(), ref(t, "example/action@"+sha)); err != nil || !manifest.Available() {
		t.Fatalf("cold=%v,%v", manifest, err)
	}
	warm, err := New(Options{CacheDir: cache, Transport: transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("warm cache used the network: %s", r.URL)
		return nil, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"example/action@" + sha, "example/action@" + strings.ToUpper(sha)} {
		manifest, err := warm.Lookup(t.Context(), ref(t, spec))
		if err != nil || !manifest.Available() || manifest.Content() != actionSource || !manifest.Selection().Route().Object() || manifest.Selection().Object() != sha {
			t.Fatalf("%s=%v,%v", spec, manifest, err)
		}
	}
}

func TestFullSHAFetchesRawManifest(t *testing.T) {
	for _, filename := range []string{"action.yml", "action.yaml"} {
		t.Run(filename, func(t *testing.T) {
			cache := t.TempDir()
			hosts := gitAndRawHosts(t, map[string]string{"/example/action/" + sha + "/nested/dir/" + filename: actionSource})
			client, err := New(Options{CacheDir: cache, Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			request := ref(t, "example/action/nested/dir@"+strings.ToUpper(sha))
			manifest, err := client.Lookup(t.Context(), request)
			want := 1
			if filename == "action.yaml" {
				want = 2
			}
			if err != nil || !manifest.Available() || manifest.Filename() != filename || !manifest.Identity().RepositoryID().IsZero() || manifest.Identity().Commit() != sha || hosts.count("raw.githubusercontent.com") != want || len(hosts.recorded()) != want {
				t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
			}
			stored, err := os.ReadFile(filepath.Join(client.bucket(request, sha), cacheFilename))
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err := json.Unmarshal(stored, &record); err != nil || record["version"] != float64(2) || record["filename"] != filename {
				t.Fatalf("record=%s,%v", stored, err)
			}
			if _, present := record["repository_id"]; present {
				t.Fatalf("raw acquisition recorded a repository ID: %s", stored)
			}
		})
	}
}

func TestNamedRefListsRefsOncePerAnalysis(t *testing.T) {
	hosts := gitAndRawHosts(t, map[string]string{
		"/example/action/" + sha + "/action.yml":           actionSource,
		"/example/action/" + sha + "/sub/action.yml":       actionSource,
		"/example/action/" + branchTip + "/sub/action.yml": actionSource,
	})
	resolver := &listingResolver{}
	client := newListingClient(t, Options{Transport: hosts}, resolver)
	for _, tc := range []struct {
		spec, route, object, commit string
	}{
		{"example/action@v1", "refs/tags/v1", tagObject, sha},
		{"example/action/sub@v1", "refs/tags/v1", tagObject, sha},
		{"example/action/sub@v2", "refs/tags/v2", sha, sha},
		{"example/action/sub@release", "refs/heads/release", branchTip, branchTip},
	} {
		manifest, err := client.Lookup(t.Context(), ref(t, tc.spec))
		if err != nil || !manifest.Available() || manifest.Selection().Route().Reference() != tc.route || manifest.Selection().Object() != tc.object || manifest.Identity().Commit() != tc.commit || !manifest.Identity().RepositoryID().IsZero() {
			t.Fatalf("%s=%v,%v requests=%v", tc.spec, manifest, err, hosts.recorded())
		}
	}
	if hosts.count("github.com") != 1 || hosts.count("raw.githubusercontent.com") != 4 || len(hosts.recorded()) != 5 {
		t.Fatalf("requests=%v", hosts.recorded())
	}
	absent, err := client.Lookup(t.Context(), ref(t, "example/action@v9"))
	if err != nil || absent.Available() || absent.Reason() != availability.FailedReason || len(hosts.recorded()) != 5 {
		t.Fatalf("absent=%v,%v requests=%v", absent, err, hosts.recorded())
	}
}

func TestNamedRefWarmCacheSkipsRawHost(t *testing.T) {
	cache := t.TempDir()
	rest := &recorder{handle: func(r *http.Request) (*http.Response, error) { return restFallback(t, r) }}
	learned, err := New(Options{CacheDir: cache, Transport: rest})
	if err != nil {
		t.Fatal(err)
	}
	if manifest, err := learned.Lookup(t.Context(), ref(t, "example/action@"+sha)); err != nil || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) {
		t.Fatalf("REST fallback=%v,%v", manifest, err)
	}
	hosts := gitAndRawHosts(t, nil)
	client := newListingClient(t, Options{CacheDir: cache, Transport: hosts}, &listingResolver{})
	manifest, err := client.Lookup(t.Context(), ref(t, "example/action@v2"))
	if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || manifest.Selection().Route().Reference() != "refs/tags/v2" || len(hosts.recorded()) != 1 || hosts.count("github.com") != 1 {
		t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
	}
}

func TestInvalidRepositoryNamesMakeNoRequests(t *testing.T) {
	client := newListingClient(t, Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("invalid repository reached the network: %s", r.URL)
		return nil, nil
	})}, &listingResolver{})
	for _, spec := range []string{"example/..@" + sha, "example/..@v1", "../action@v1"} {
		manifest, err := client.Lookup(t.Context(), ref(t, spec))
		if err != nil || manifest.Reason() != availability.FailedReason {
			t.Fatalf("%s=%v,%v", spec, manifest, err)
		}
	}
}

func TestRefListingFreshPerWithResolver(t *testing.T) {
	hosts := gitAndRawHosts(t, map[string]string{"/example/action/" + sha + "/action.yml": actionSource})
	base, err := New(Options{Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	resolver := repository.Bind(&listingResolver{}, base)
	for analysis := range 2 {
		provider := base.WithResolver(resolver)
		for range 2 {
			if manifest, err := provider.Lookup(t.Context(), ref(t, "example/action@v1")); err != nil || !manifest.Available() {
				t.Fatalf("analysis %d: %v,%v", analysis, manifest, err)
			}
		}
		if hosts.count("github.com") != analysis+1 {
			t.Fatalf("analysis %d listings: %v", analysis, hosts.recorded())
		}
	}
}

func TestParseRefAdvertisement(t *testing.T) {
	packet := func(payload string) string { return fmt.Sprintf("%04x%s", len(payload)+4, payload) }
	header := packet("# service=git-upload-pack\n") + "0000"
	first := packet(branchTip + " HEAD\x00symref=HEAD:refs/heads/main object-format=sha1 agent=git/2\n")
	valid := listing(branchTip, gitwire.Ref{Name: "refs/heads/main", Object: branchTip}, gitwire.Ref{Name: "refs/tags/v1", Object: tagObject, Peeled: sha}, gitwire.Ref{Name: "refs/pull/1/head", Object: sha})
	parsed, err := parseRefAdvertisement([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.refs["HEAD"] != branchTip || parsed.refs["refs/tags/v1"] != tagObject || parsed.refs["refs/pull/1/head"] != sha || len(parsed.refs) != 4 {
		t.Fatalf("refs=%v", parsed.refs)
	}
	if parsed.objects[tagObject] != (repository.PeeledTag{Commit: sha}) || parsed.objects[sha] != (repository.Commit{}) || parsed.objects[branchTip] != (repository.Commit{}) || len(parsed.digest) != 64 {
		t.Fatalf("objects=%v digest=%s", parsed.objects, parsed.digest)
	}
	empty, err := parseRefAdvertisement(gitwire.RefAdvertisement("", ""))
	if err != nil || len(empty.refs) != 0 || len(empty.objects) != 0 {
		t.Fatalf("empty=%v,%v", empty, err)
	}
	upper, err := parseRefAdvertisement([]byte(strings.ToUpper(valid[:4]) + valid[4:]))
	if err != nil || len(upper.refs) != 4 {
		t.Fatalf("uppercase length=%v,%v", upper, err)
	}
	for name, body := range map[string]string{
		"empty body":          "",
		"missing header":      "0000",
		"conflicting peels":   header + first + packet(tagObject+" refs/tags/v1\n") + packet(sha+" refs/tags/v1^{}\n") + packet(tagObject+" refs/tags/v1-copy\n") + packet(branchTip+" refs/tags/v1-copy^{}\n") + "0000",
		"wrong service":       packet("# service=git-receive-pack\n") + "0000" + first + "0000",
		"no flush after head": packet("# service=git-upload-pack\n") + first + "0000",
		"bad length":          header + "zzzz",
		"short length":        header + "0003",
		"truncated":           header + "00ff" + branchTip,
		"no capabilities":     header + packet(branchTip+" HEAD\n") + "0000",
		"duplicate":           header + first + packet(sha+" HEAD\n") + "0000",
		"orphan peel":         header + first + packet(sha+" refs/tags/v1^{}\n") + "0000",
		"peel after peel":     header + first + packet(tagObject+" refs/tags/v1\n") + packet(sha+" refs/tags/v1^{}\n") + packet(sha+" refs/tags/v1^{}\n") + "0000",
		"uppercase object":    header + first + packet(strings.ToUpper(sha)+" refs/tags/v1\n") + "0000",
		"missing final flush": header + first,
		"trailing bytes":      valid + "0000",
		"empty then ref":      string(gitwire.RefAdvertisement("", "")[:len(gitwire.RefAdvertisement("", ""))-4]) + packet(sha+" refs/heads/main\n") + "0000",
		"peeled tag target":   header + first + packet(tagObject+" refs/tags/v1\n") + packet(sha+" refs/tags/v1^{}\n") + packet(sha+" refs/tags/v2\n") + packet(branchTip+" refs/tags/v2^{}\n") + "0000",
	} {
		if _, err := parseRefAdvertisement([]byte(body)); !errors.Is(err, errInvalidRefAdvertisement) {
			t.Errorf("%s: error=%v", name, err)
		}
	}
	sha256Format := header + packet(strings.Repeat("a", 64)+" HEAD\x00object-format=sha256\n") + "0000"
	if _, err := parseRefAdvertisement([]byte(sha256Format)); !errors.Is(err, errUnsupportedObjectIDs) {
		t.Fatalf("sha256 listing error=%v", err)
	}
}

func TestListingSessionFinishMakesNoRequests(t *testing.T) {
	hosts := gitAndRawHosts(t, nil)
	client, err := New(Options{Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	listed, missing, err := client.listings.get(t.Context(), client, "example/action")
	if err != nil || missing != nil {
		t.Fatalf("listing=%v,%v", missing, err)
	}
	ctx := withRefListing(t.Context(), listed)
	if _, err := client.Open(ctx, references.Request{Version: 1, Repository: "example/action", Ref: "v1", Kind: "action"}); err != nil {
		t.Fatal(err)
	}
	session, err := client.Open(ctx, references.Request{Version: 1, Repository: "Example/Action", Ref: "v1", Kind: "action"})
	if err != nil {
		t.Fatal(err)
	}
	if identity := session.Repository(); identity.Host != "github.com" || identity.Name != "Example/Action" || !identity.ID.IsZero() || identity.ObservedAt == "" {
		t.Fatalf("identity=%+v", identity)
	}
	if id, found, err := session.Reference(t.Context(), "refs/v1"); err != nil || found || id != "" {
		t.Fatalf("absent reference=%s,%t,%v", id, found, err)
	}
	if id, found, err := session.Reference(t.Context(), "HEAD"); err != nil || !found || id != branchTip {
		t.Fatalf("HEAD=%s,%t,%v", id, found, err)
	}
	if _, _, err := session.Reference(t.Context(), "v1"); err == nil {
		t.Fatal("short reference name accepted")
	}
	if object, found, err := session.Object(t.Context(), strings.ToUpper(tagObject)); err != nil || !found || object != (repository.PeeledTag{Commit: sha}) {
		t.Fatalf("tag object=%v,%t,%v", object, found, err)
	}
	if _, found, err := session.Object(t.Context(), strings.Repeat("d", 40)); err != nil || found {
		t.Fatalf("unknown object=%t,%v", found, err)
	}
	for _, id := range []string{strings.Repeat("a", 64), "abc"} {
		if _, _, err := session.Object(t.Context(), id); err == nil {
			t.Fatalf("object %q accepted", id)
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := session.Reference(canceled, "HEAD"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reference=%v", err)
	}
	if _, _, err := session.Object(canceled, sha); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled object=%v", err)
	}
	if _, err := session.Finish(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finish=%v", err)
	}
	log, err := session.Finish(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var observations struct {
		Host         string   `json:"host"`
		Observations []string `json:"observations"`
	}
	if err := json.Unmarshal(log, &observations); err != nil || observations.Host != "github.com" || observations.Observations[0] != "listing sha256:"+listed.digest || !slices.Contains(observations.Observations, "reference HEAD") || !slices.Contains(observations.Observations, "object "+tagObject) {
		t.Fatalf("log=%s,%v", log, err)
	}
	if len(hosts.recorded()) != 1 {
		t.Fatalf("session made requests: %v", hosts.recorded())
	}
	if _, _, err := session.Reference(t.Context(), "HEAD"); err == nil {
		t.Fatal("finished session answered a reference")
	}
	if _, _, err := session.Object(t.Context(), sha); err == nil {
		t.Fatal("finished session answered an object")
	}
	if _, err := session.Finish(t.Context()); err == nil {
		t.Fatal("session finished twice")
	}
	apiRequests := 0
	other, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" || r.URL.Path != "/repos/other/action" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		apiRequests++
		return respond(r, http.StatusNotFound, "", ""), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(ctx, references.Request{Version: 1, Repository: "other/action"}); err == nil || apiRequests != 1 {
		t.Fatalf("listing of another repository answered an open request: %v requests=%d", err, apiRequests)
	}
}

type failure struct {
	name        string
	status      int
	contentType string
	body        string
	connection  bool
}

func environmentFailures() []failure {
	return []failure{
		{name: "connection", connection: true},
		{name: "500", status: http.StatusInternalServerError},
		{name: "503", status: http.StatusServiceUnavailable},
		{name: "401", status: http.StatusUnauthorized},
		{name: "403", status: http.StatusForbidden},
		{name: "429", status: http.StatusTooManyRequests},
		{name: "content type", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: "<html>portal</html>"},
		{name: "malformed content type", status: http.StatusOK, contentType: "/", body: "portal"},
	}
}

func (f failure) respond(r *http.Request) (*http.Response, error) {
	if f.connection {
		return nil, errors.New("dial tcp: connection refused")
	}
	return respond(r, f.status, f.contentType, f.body), nil
}

func TestEnvironmentFailuresFallBackToAPI(t *testing.T) {
	oversized := failure{name: "oversized listing", status: http.StatusOK, contentType: gitwire.ContentType, body: strings.Repeat("0", maxListing+1)}
	unparseable := failure{name: "unparseable listing", status: http.StatusOK, contentType: gitwire.ContentType, body: "0000"}
	for _, target := range []string{"git", "raw"} {
		failures := environmentFailures()
		if target == "git" {
			failures = append(failures, oversized, unparseable)
		}
		for _, f := range failures {
			t.Run(target+"/"+f.name, func(t *testing.T) {
				hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
					switch {
					case target == "git" && r.URL.Host == "github.com", target == "raw" && r.URL.Host == "raw.githubusercontent.com":
						return f.respond(r)
					case r.URL.Host == "github.com":
						return respond(r, http.StatusOK, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/tags/v1", Object: sha})), nil
					}
					return restFallback(t, r)
				}}
				resolutions := 0
				client, err := New(Options{Transport: hosts, Resolver: func() references.Resolver {
					return resolveFunc(func(ctx context.Context, r references.Request) (references.Result, error) {
						resolutions++
						return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
					})
				}})
				if err != nil {
					t.Fatal(err)
				}
				spec := "example/action@" + sha
				if target == "git" {
					spec = "example/action@v1"
				}
				manifest, err := client.Lookup(t.Context(), ref(t, spec))
				if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || hosts.count("api.github.com") == 0 {
					t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
				}
				if target == "git" && resolutions != 1 {
					t.Fatalf("resolutions=%d", resolutions)
				}
			})
		}
	}
}

func TestDefiniteAnswersNeverReachAPI(t *testing.T) {
	for _, tc := range []struct {
		name, spec  string
		git, raw    int
		want        availability.Reason
		requests    int
		withListing bool
	}{
		{"git 404", "example/action@v1", http.StatusNotFound, 0, availability.InaccessibleReason, 1, false},
		{"git 302", "example/action@v1", http.StatusFound, 0, availability.FailedReason, 1, false},
		{"git 410", "example/action@v1", http.StatusGone, 0, availability.FailedReason, 1, false},
		{"absent ref", "example/action@v9", http.StatusOK, 0, availability.FailedReason, 1, true},
		{"raw 404", "example/action@" + sha, http.StatusOK, http.StatusNotFound, availability.InaccessibleReason, 3, true},
		{"raw 404 and git 404", "example/action@" + sha, http.StatusNotFound, http.StatusNotFound, availability.InaccessibleReason, 3, false},
		{"raw 302", "example/action@" + sha, 0, http.StatusFound, availability.FailedReason, 1, false},
		{"named raw 404", "example/action@v1", http.StatusOK, http.StatusNotFound, availability.InaccessibleReason, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				switch r.URL.Host {
				case "github.com":
					if tc.withListing {
						return respond(r, tc.git, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/tags/v1", Object: sha})), nil
					}
					return respond(r, tc.git, "text/html", ""), nil
				case "raw.githubusercontent.com":
					return respond(r, tc.raw, rawText, ""), nil
				}
				t.Fatalf("definite answer reached %s", r.URL)
				return nil, nil
			}}
			client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
			manifest, err := client.Lookup(t.Context(), ref(t, tc.spec))
			if err != nil || manifest.Available() || manifest.Reason() != tc.want || len(hosts.recorded()) != tc.requests {
				t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
			}
		})
	}
}

func TestAnnotatedTagObjectSHAUsesListingAndPeeledCommit(t *testing.T) {
	cache := t.TempDir()
	hosts := gitAndRawHosts(t, map[string]string{"/example/action/" + sha + "/action.yml": actionSource})
	resolver := &listingResolver{}
	client := newListingClient(t, Options{CacheDir: cache, Transport: hosts}, resolver)
	for _, spec := range []string{"example/action@" + tagObject, "example/action@" + strings.ToUpper(tagObject)} {
		manifest, err := client.Lookup(t.Context(), ref(t, spec))
		if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().Commit() != sha || manifest.Selection().Object() != tagObject || !manifest.Selection().Route().Object() || !manifest.Identity().RepositoryID().IsZero() {
			t.Fatalf("%s=%v,%v requests=%v", spec, manifest, err, hosts.recorded())
		}
	}
	tagManifest := "raw.githubusercontent.com/example/action/" + tagObject
	want := []string{
		tagManifest + "/action.yml",
		tagManifest + "/action.yaml",
		"github.com/example/action.git/info/refs?service=git-upload-pack",
		"raw.githubusercontent.com/example/action/" + sha + "/action.yml",
	}
	if !slices.Equal(hosts.recorded(), want) || resolver.resolutions() != 1 {
		t.Fatalf("requests=%v resolutions=%d", hosts.recorded(), resolver.resolutions())
	}
	offline, err := New(Options{Mode: Offline, CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []actions.Provider{newListingClient(t, Options{CacheDir: cache, Transport: hosts}, resolver), offline} {
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+tagObject))
		if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().Commit() != sha || manifest.Selection().Object() != tagObject || !manifest.Selection().Route().Object() {
			t.Fatalf("cached tag object=%v,%v", manifest, err)
		}
	}
	if !slices.Equal(hosts.recorded(), want) || resolver.resolutions() != 1 {
		t.Fatalf("cached tag object made requests=%v resolutions=%d", hosts.recorded(), resolver.resolutions())
	}
	commit, err := offline.Lookup(t.Context(), ref(t, "example/action@"+sha))
	if err != nil || !commit.Available() || commit.Selection().Object() != sha {
		t.Fatalf("commit record=%v,%v", commit, err)
	}
}

func TestMissingSHAWithUnavailableListingFallsBackToAPI(t *testing.T) {
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "raw.githubusercontent.com":
			return respond(r, http.StatusNotFound, rawText, "404: Not Found"), nil
		case "github.com":
			return respond(r, http.StatusServiceUnavailable, "", ""), nil
		}
		return restFallback(t, r)
	}}
	client, err := New(Options{Transport: hosts, Resolver: func() references.Resolver {
		return resolveFunc(func(ctx context.Context, r references.Request) (references.Result, error) {
			if refListingFromContext(ctx) != nil || r.ExpectedRepositoryID != knownRepositoryID(t, 42) {
				t.Fatalf("fallback resolution used the listing or lost the learned ID: %+v", r)
			}
			return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
	if err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || hosts.count("raw.githubusercontent.com") != 2 || hosts.count("github.com") != 1 || hosts.count("api.github.com") != 4 {
		t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
	}
}

func TestSHA256RepositoryListingIsUnsupported(t *testing.T) {
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "github.com" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		body := fmt.Sprintf("%04x# service=git-upload-pack\n0000%04x%s HEAD\x00object-format=sha256\n0000", 30, 4+len(strings.Repeat("a", 64)+" HEAD\x00object-format=sha256\n"), strings.Repeat("a", 64))
		return respond(r, http.StatusOK, gitwire.ContentType, body), nil
	}}
	client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
	manifest, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
	if err != nil || manifest.Reason() != availability.UnsupportedObjectFormatReason || len(hosts.recorded()) != 1 {
		t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
	}
}

func TestPreflightOnlyOnFallback(t *testing.T) {
	for _, rawStatus := range []int{http.StatusOK, http.StatusBadGateway} {
		t.Run(http.StatusText(rawStatus), func(t *testing.T) {
			ctx, _ := accounting.TrackRequests(t.Context())
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				switch {
				case r.URL.Host == "raw.githubusercontent.com":
					return respond(r, rawStatus, rawText, actionSource), nil
				case r.URL.Path == "/rate_limit":
					return respond(r, http.StatusOK, "", fmt.Sprintf(`{"resources":{"core":{"remaining":100,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())), nil
				}
				return restFallback(t, r)
			}}
			client, err := New(Options{Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(ctx, ref(t, "example/action@"+sha))
			if err != nil || !manifest.Available() {
				t.Fatalf("manifest=%v,%v", manifest, err)
			}
			probes := slices.Index(hosts.recorded(), "api.github.com/rate_limit")
			if rawStatus == http.StatusOK && (probes != -1 || len(hosts.recorded()) != 1) || rawStatus != http.StatusOK && (probes != 2 || hosts.count("cdn.jsdelivr.net") != 1) {
				t.Fatalf("requests=%v", hosts.recorded())
			}
		})
	}
}

func TestRequestAccountingLabelsGitAndRaw(t *testing.T) {
	ctx, tracker := accounting.TrackRequests(t.Context())
	hosts := gitAndRawHosts(t, map[string]string{"/example/action/" + sha + "/action.yml": actionSource})
	client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
	if manifest, err := client.Lookup(ctx, ref(t, "example/action@v1")); err != nil || !manifest.Available() {
		t.Fatalf("manifest=%v,%v", manifest, err)
	}
	if workflow, err := client.LookupWorkflow(ctx, "example/action/.github/workflows/missing.yml@v2"); err != nil || workflow.Available() {
		t.Fatalf("workflow=%+v,%v", workflow, err)
	}
	type group struct {
		host     string
		kind     accounting.RequestKind
		purpose  string
		attempts uint64
		failures uint64
	}
	var groups []group
	for _, counts := range tracker.Statistics().Requests {
		if counts.Priority != accounting.Enrichment || counts.Credentials != accounting.NoCredentials {
			t.Fatalf("group=%+v", counts)
		}
		groups = append(groups, group{counts.Host, counts.RequestKind, counts.Purpose, counts.Attempts, counts.Failures})
	}
	want := []group{
		{"github.com", accounting.Git, "resolver", 1, 0},
		{"raw.githubusercontent.com", accounting.HTTP, "action-metadata", 1, 0},
		{"raw.githubusercontent.com", accounting.HTTP, "workflow-metadata", 1, 1},
	}
	if !slices.Equal(groups, want) {
		t.Fatalf("groups=%+v, want %+v", groups, want)
	}
}

func TestCredentialsForGitRawAndJSDelivr(t *testing.T) {
	const token = "secret-token"
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	cache := t.TempDir()
	anonymousRetries := 0
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.String(), token) {
			t.Fatalf("token in URL %s", r.URL)
		}
		if strings.HasPrefix(r.URL.Path, "/mirrored/") {
			return respond(r, http.StatusServiceUnavailable, "", ""), nil
		}
		switch r.URL.Host {
		case "cdn.jsdelivr.net":
			if r.Header.Get("Authorization") != "" || r.URL.User != nil {
				t.Fatalf("credentials sent to jsDelivr: %q %s", r.Header.Get("Authorization"), r.URL)
			}
			return respond(r, http.StatusOK, jsDelivrYAML, actionSource), nil
		case "github.com":
			if r.Header.Get("Authorization") != basic {
				t.Fatalf("git authorization=%q", r.Header.Get("Authorization"))
			}
			if strings.HasPrefix(r.URL.Path, "/private/") {
				return respond(r, http.StatusUnauthorized, "text/plain", token), nil
			}
			return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
		case "raw.githubusercontent.com":
			switch r.Header.Get("Authorization") {
			case "Bearer " + token:
				return respond(r, http.StatusNotFound, rawText, token), nil
			case "":
				anonymousRetries++
				return respond(r, http.StatusOK, rawText, actionSource), nil
			}
			t.Fatalf("raw authorization=%q", r.Header.Get("Authorization"))
		case "api.github.com":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("API authorization=%q", r.Header.Get("Authorization"))
			}
			return respond(r, http.StatusUnauthorized, "", token), nil
		}
		t.Fatalf("unexpected authority %s", r.URL)
		return nil, nil
	}}
	client := newListingClient(t, Options{Token: token, CacheDir: cache, Transport: hosts}, &listingResolver{})
	manifest, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
	if err != nil || !manifest.Available() || anonymousRetries != 1 {
		t.Fatalf("manifest=%v,%v retries=%d requests=%v", manifest, err, anonymousRetries, hosts.recorded())
	}
	denied, err := client.Lookup(t.Context(), ref(t, "private/action@v1"))
	if err != nil || denied.Reason() != availability.DeniedReason || strings.Contains(string(denied.Reason()), token) {
		t.Fatalf("denied=%v,%v", denied, err)
	}
	stored, err := os.ReadFile(filepath.Join(client.bucket(ref(t, "example/action@v1"), sha), cacheFilename))
	if err != nil || bytes.Contains(stored, []byte(token)) {
		t.Fatalf("cache=%s,%v", stored, err)
	}
	if _, _, err := client.fetchRefListing(t.Context(), "private/action"); !errors.Is(err, errEnvironment) || strings.Contains(err.Error(), token) {
		t.Fatalf("listing error=%v", err)
	}
	mirrored, err := client.Lookup(t.Context(), ref(t, "mirrored/action@"+sha))
	if err != nil || !mirrored.Available() || hosts.count("cdn.jsdelivr.net") != 1 {
		t.Fatalf("mirrored=%v,%v requests=%v", mirrored, err, hosts.recorded())
	}
}

func TestRefListingWaiterRetriesAfterCanceledInitiator(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		requests := 0
		client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		initiator, cancel := context.WithCancel(t.Context())
		var initiatorErr, waiterErr error
		var waiterListing *refListing
		done := make(chan struct{}, 2)
		go func() {
			_, _, initiatorErr = client.listings.get(initiator, client, "example/action")
			done <- struct{}{}
		}()
		synctest.Wait()
		go func() {
			waiterListing, _, waiterErr = client.listings.get(t.Context(), client, "Example/Action")
			done <- struct{}{}
		}()
		synctest.Wait()
		if requests != 1 {
			t.Fatalf("waiter started its own listing: %d requests", requests)
		}
		cancel()
		<-done
		<-done
		if !errors.Is(initiatorErr, context.Canceled) || waiterErr != nil || waiterListing == nil || waiterListing.refs["refs/tags/v1"] != tagObject || requests != 2 {
			t.Fatalf("initiator=%v waiter=%v,%v requests=%d", initiatorErr, waiterListing, waiterErr, requests)
		}
		if cached, _, err := client.listings.get(t.Context(), client, "example/action"); err != nil || cached != waiterListing || requests != 2 {
			t.Fatalf("listing not shared after retry: %v,%v requests=%d", cached, err, requests)
		}
		waiting, stop := context.WithCancel(t.Context())
		blocked := make(chan struct{})
		entry := &refListingEntry{ready: blocked}
		client.listings.entries["blocked/action"] = entry
		stop()
		if _, _, err := client.listings.get(waiting, client, "blocked/action"); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter=%v", err)
		}
	})
}

func TestRefListingWaitIsOutsideLookupBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
			time.Sleep(10 * time.Second)
			return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		initiated := make(chan *refListing, 1)
		go func() {
			listed, _, _ := client.listings.get(t.Context(), client, "example/action")
			initiated <- listed
		}()
		synctest.Wait()
		budgeted, stop := references.StartLookupBudget(t.Context(), time.Second)
		defer stop()
		listed, missing, err := client.listings.get(budgeted, client, "example/action")
		shared := <-initiated
		if err != nil || missing != nil || listed == nil || listed != shared || budgeted.Err() != nil {
			t.Fatalf("listing=%v,%v,%v budget=%v", listed, missing, err, context.Cause(budgeted))
		}
	})
}

func TestEnvironmentListingFailureIsSharedPerAnalysis(t *testing.T) {
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "github.com" {
			return restFallback(t, r)
		}
		return respond(r, http.StatusServiceUnavailable, "", ""), nil
	}}
	client, err := New(Options{Transport: hosts, Resolver: func() references.Resolver {
		return resolveFunc(func(context.Context, references.Request) (references.Result, error) {
			return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{"example/action@v1", "example/action@v2"} {
		if manifest, err := client.Lookup(t.Context(), ref(t, spec)); err != nil || !manifest.Available() {
			t.Fatalf("%s=%v,%v", spec, manifest, err)
		}
	}
	if hosts.count("github.com") != 1 {
		t.Fatalf("listing retried within one analysis: %v", hosts.recorded())
	}
}

func blackholedRawHost(r *http.Request) (*http.Response, error) {
	switch {
	case r.URL.Host == "raw.githubusercontent.com":
		<-r.Context().Done()
		return nil, r.Context().Err()
	case strings.HasSuffix(r.URL.Path, "/git/commits/"+sha):
		return respond(r, http.StatusOK, "", `{"sha":"`+sha+`"}`), nil
	case strings.HasSuffix(r.URL.Path, "/contents/action.yml"):
		return respond(r, http.StatusOK, "", actionSource), nil
	}
	return respond(r, http.StatusOK, "", `{"id":42,"full_name":"`+strings.TrimPrefix(r.URL.Path, "/repos/")+`"}`), nil
}

func TestQueuedLookupsKeepTheirBudgetForAPIFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hosts := &recorder{handle: blackholedRawHost}
		client, err := New(Options{Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		lookups := 5*concurrentTransfers/2 + 1
		var group sync.WaitGroup
		for i := range lookups {
			group.Go(func() {
				spec := fmt.Sprintf("example/action%d@%s", i, sha)
				manifest, err := client.Lookup(t.Context(), ref(t, spec))
				if err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) {
					t.Errorf("%s=%v,%v", spec, manifest, err)
				}
			})
		}
		group.Wait()
		if hosts.count("raw.githubusercontent.com") != lookups || hosts.count("api.github.com") != 4*lookups {
			t.Fatalf("requests=%v", hosts.recorded())
		}
	})
}

func TestExhaustedLookupBudgetIsUnavailability(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(14 * time.Second):
			}
			if r.URL.Host == "raw.githubusercontent.com" {
				return respond(r, http.StatusServiceUnavailable, "", ""), nil
			}
			return blackholedRawHost(r)
		})})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		if err != nil || manifest.Reason() != availability.FailedReason || time.Since(start) != lookupBudget {
			t.Fatalf("manifest=%v,%v after %s", manifest, err, time.Since(start))
		}
		parent, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(time.Second)
			cancel()
		}()
		if _, err := client.Lookup(parent, ref(t, "example/action@"+sha)); !errors.Is(err, context.Canceled) {
			t.Fatalf("caller cancellation=%v", err)
		}
	})
}

func TestLookupsForDifferentRepositoriesRunConcurrently(t *testing.T) {
	var mu sync.Mutex
	inFlight := 0
	released := make(chan struct{})
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		inFlight++
		if inFlight == 2 {
			close(released)
		}
		mu.Unlock()
		select {
		case <-released:
		case <-time.After(5 * time.Second):
			t.Error("metadata lookups for different repositories ran one after another")
		}
		return respond(r, http.StatusOK, rawText, actionSource), nil
	}}
	client, err := New(Options{Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, repository := range []string{"example/first", "example/second"} {
		group.Go(func() {
			if manifest, err := client.Lookup(t.Context(), ref(t, repository+"@"+sha)); err != nil || !manifest.Available() {
				t.Errorf("%s=%v,%v", repository, manifest, err)
			}
		})
	}
	group.Wait()
}
