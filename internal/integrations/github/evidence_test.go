package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type resolveFunc func(context.Context, references.Request) (references.Result, error)

func (f resolveFunc) Resolve(ctx context.Context, r references.Request) (references.Result, error) {
	return f(ctx, r)
}

func TestNamedMetadataRequiresActionSelectionEvidence(t *testing.T) {
	t.Parallel()
	for _, selectedRef := range []string{"", "refs/heads/v1"} {
		client, err := New(Options{Mode: Online, Resolver: func() references.Resolver {
			return resolveFunc(func(ctx context.Context, r references.Request) (references.Result, error) {
				if r.Kind != "action" || !r.ExpectedRepositoryID.IsZero() || refListingFromContext(ctx) == nil {
					t.Fatalf("wrong profile, guard or listing: %+v", r)
				}
				return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: selectedRef, ObservedAt: "2026-10-31T00:00:00Z", FetchLog: json.RawMessage(`{"profile":"GithubAction"}`)}, nil
			})
		}, Transport: transport(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host + r.URL.Path {
			case "github.com/example/action.git/info/refs":
				return respond(r, http.StatusOK, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/heads/v1", Object: sha})), nil
			case "raw.githubusercontent.com/example/action/" + sha + "/action.yml":
				return respond(r, http.StatusOK, rawText, actionSource), nil
			}
			t.Fatalf("unexpected endpoint %s", r.URL)
			return nil, nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
		if err != nil {
			t.Fatal(err)
		}
		if selectedRef == "" {
			if result.Reason() != availability.InvalidResolverResultReason {
				t.Fatalf("guessed route: %v", result)
			}
		} else if !result.Available() || result.Selection().Route().Reference() != selectedRef {
			t.Fatalf("lost route: %v", result)
		}
	}
}

func TestInvalidResolverResultsDoNotFetchManifests(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		change   func(*references.Result)
		fallback bool
	}{
		{"version", func(r *references.Result) { r.Version = 2 }, false},
		{"error", func(r *references.Result) { r.Error = "resolution failed" }, false},
		{"repository ID", func(r *references.Result) { r.RepositoryID = knownRepositoryID(t, 99) }, true},
		{"repository name", func(r *references.Result) { r.Repository = "another/action" }, false},
		{"commit", func(r *references.Result) { r.Commit = "short" }, false},
		{"tree", func(r *references.Result) { r.Tree = sha }, false},
		{"observation", func(r *references.Result) { r.ObservedAt = "" }, false},
		{"fetch log", func(r *references.Result) { r.FetchLog = nil }, false},
		{"selected object", func(r *references.Result) { r.SelectedObject = "" }, false},
		{"selected ref", func(r *references.Result) { r.SelectedRef = "" }, false},
	} {
		for _, fallback := range []bool{false, true} {
			if tc.fallback && !fallback {
				continue
			}
			t.Run(fmt.Sprintf("%s/fallback=%t", tc.name, fallback), func(t *testing.T) {
				t.Parallel()
				hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
					switch r.URL.Host + r.URL.Path {
					case "github.com/example/action.git/info/refs":
						if fallback {
							return respond(r, http.StatusServiceUnavailable, "", ""), nil
						}
						return respond(r, http.StatusOK, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/tags/v1", Object: sha})), nil
					case "api.github.com/repos/example/action":
						if fallback {
							return respond(r, http.StatusOK, "", `{"id":42,"full_name":"example/action"}`), nil
						}
					}
					t.Fatalf("invalid result fetched %s", r.URL)
					return nil, nil
				}}
				client, err := New(Options{Resolver: func() references.Resolver {
					return resolveFunc(func(context.Context, references.Request) (references.Result, error) {
						result := references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-10-31T00:00:00Z", FetchLog: json.RawMessage(`{"profile":"GithubAction"}`)}
						tc.change(&result)
						return result, nil
					})
				}, Transport: hosts})
				if err != nil {
					t.Fatal(err)
				}
				result, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
				want := 1
				if fallback {
					want = 2
				}
				if err != nil || result.Reason() != availability.InvalidResolverResultReason || len(hosts.recorded()) != want {
					t.Fatalf("result=%v error=%v requests=%v", result, err, hosts.recorded())
				}
			})
		}
	}
}

func TestMissingObjectNeverFallsBackToNamedTag(t *testing.T) {
	t.Parallel()
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "raw.githubusercontent.com" {
			t.Fatalf("object fallback: %s", r.URL)
		}
		return respond(r, http.StatusNotFound, rawText, "404: Not Found"), nil
	}}
	client, err := New(Options{Mode: Online, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
	if err != nil || m.Reason() != availability.InaccessibleReason || len(hosts.recorded()) != 2 {
		t.Fatalf("missing object=%v,%v requests=%v", m, err, hosts.recorded())
	}
	listed := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "raw.githubusercontent.com":
			return respond(r, http.StatusNotFound, rawText, "404: Not Found"), nil
		case "github.com":
			return respond(r, http.StatusOK, gitwire.ContentType, listing(branchTip, gitwire.Ref{Name: "refs/tags/" + sha, Object: branchTip})), nil
		}
		t.Fatalf("object fallback: %s", r.URL)
		return nil, nil
	}}
	resolver := &listingResolver{}
	withResolver := newListingClient(t, Options{Transport: listed}, resolver)
	m, err = withResolver.Lookup(t.Context(), ref(t, "example/action@"+sha))
	if err != nil || m.Reason() != availability.InaccessibleReason || len(listed.recorded()) != 3 || resolver.resolutions() != 0 {
		t.Fatalf("missing object with resolver=%v,%v requests=%v resolutions=%d", m, err, listed.recorded(), resolver.resolutions())
	}
}

func TestManifestFallbackLimitAndIdentityGuard(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"yaml", "oversized", "identity"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			repoReads := 0
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, actionSource
				switch r.URL.Host + r.URL.Path {
				case "raw.githubusercontent.com/example/action/" + sha + "/action.yml":
					switch scenario {
					case "yaml":
						status = http.StatusNotFound
					case "oversized":
						body = strings.Repeat("x", maxBody+1)
					case "identity":
						status = http.StatusServiceUnavailable
					}
					return respond(r, status, rawText, body), nil
				case "raw.githubusercontent.com/example/action/" + sha + "/action.yaml":
					if scenario != "yaml" {
						t.Fatal("unexpected fallback")
					}
					return respond(r, status, rawText, body), nil
				case "cdn.jsdelivr.net/gh/example/action@" + sha + "/action.yml":
					if scenario != "identity" {
						t.Fatalf("definite raw answer reached jsDelivr: %s", r.URL)
					}
					return respond(r, http.StatusServiceUnavailable, "", ""), nil
				case "api.github.com/repos/example/action":
					repoReads++
					body = `{"id":42,"full_name":"example/action"}`
					if repoReads > 1 {
						body = `{"id":99,"full_name":"example/action"}`
					}
				case "api.github.com/repos/example/action/git/commits/" + sha:
					body = `{"sha":"` + sha + `"}`
				case "api.github.com/repos/example/action/contents/action.yml":
				default:
					t.Fatalf("unexpected request %s", r.URL)
				}
				if scenario != "identity" {
					t.Fatalf("definite raw answer reached the API: %s", r.URL)
				}
				return respond(r, status, "", body), nil
			}}
			client, err := New(Options{Mode: Online, Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "yaml":
				if !m.Available() || m.Filename() != "action.yaml" || len(hosts.recorded()) != 2 {
					t.Fatalf("yaml=%v requests=%v", m, hosts.recorded())
				}
			case "oversized":
				if m.Reason() != availability.LimitReason {
					t.Fatalf("oversized=%v", m)
				}
			case "identity":
				if m.Available() || repoReads != 2 {
					t.Fatalf("identity change accepted: %v reads=%d", m, repoReads)
				}
			}
		})
	}
}

func TestOfflineRejectsCorruptionAndIgnoresPreviousLayout(t *testing.T) {
	t.Parallel()
	client, err := New(Options{Mode: Offline, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action@"+sha)
	bucket := client.bucket(request, sha)
	if err := os.MkdirAll(bucket, 0700); err != nil {
		t.Fatal(err)
	}
	previous := cacheRecordJSON(1, `"repository_id":42,`, actionSource, digest(actionSource))
	if err := os.WriteFile(filepath.Join(bucket, "42.json"), []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := client.Lookup(t.Context(), request)
	if err != nil || result.Reason() != availability.CacheMissReason {
		t.Fatalf("previous layout=%v,%v", result, err)
	}
	for name, record := range map[string]string{
		"previous version": previous,
		"tampered content": cacheRecordJSON(2, "", "tampered", digest(actionSource)),
		"zero ID":          cacheRecordJSON(2, `"repository_id":0,`, actionSource, digest(actionSource)),
		"unparseable":      `{"version":2,"content":`,
	} {
		if err := os.WriteFile(filepath.Join(bucket, cacheFilename), []byte(record), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := client.Lookup(t.Context(), request)
		if err != nil || result.Reason() != availability.FailedReason {
			t.Fatalf("%s=%v,%v", name, result, err)
		}
	}
}

func cacheRecordJSON(version int, repositoryID, content, sha256 string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"version":%d,"host":"github.com","repository":"example/action",%s"commit":%q,"directory":"","observed_at":"2026-11-12T00:00:00Z","filename":"action.yml","content":%s,"sha256":%q}`, version, repositoryID, sha, encoded, sha256)
}

func restFallback(t *testing.T, r *http.Request) (*http.Response, error) {
	t.Helper()
	switch r.URL.Host + r.URL.Path {
	case "github.com/example/action.git/info/refs":
		return respond(r, http.StatusServiceUnavailable, "", ""), nil
	case "raw.githubusercontent.com/example/action/" + sha + "/action.yml", "cdn.jsdelivr.net/gh/example/action@" + sha + "/action.yml":
		return respond(r, http.StatusServiceUnavailable, "", ""), nil
	case "api.github.com/repos/example/action":
		return respond(r, http.StatusOK, "", `{"id":42,"full_name":"example/action"}`), nil
	case "api.github.com/repos/example/action/git/commits/" + sha:
		return respond(r, http.StatusOK, "", `{"sha":"`+sha+`"}`), nil
	case "api.github.com/repos/example/action/contents/action.yml":
		if r.URL.Query().Get("ref") != sha {
			t.Fatalf("manifest not pinned: %s", r.URL)
		}
		return respond(r, http.StatusOK, "", actionSource), nil
	}
	t.Fatalf("unexpected request %s", r.URL)
	return nil, nil
}

func TestCacheRecordRepositoryIDIsOptional(t *testing.T) {
	t.Parallel()
	cache := t.TempDir()
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) { return restFallback(t, r) }}
	online, err := New(Options{CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	offline, err := New(Options{Mode: Offline, CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action@"+sha)
	path := filepath.Join(online.bucket(request, sha), cacheFilename)
	if manifest, err := online.Lookup(t.Context(), request); err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || hosts.count("api.github.com") != 4 || hosts.count("cdn.jsdelivr.net") != 0 || len(hosts.recorded()) != 5 {
		t.Fatalf("online=%v,%v requests=%v", manifest, err, hosts.recorded())
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record cacheRecord
	if err := json.Unmarshal(stored, &record); err != nil || record.Version != 2 || record.RepositoryID != knownRepositoryID(t, 42) {
		t.Fatalf("stored record=%+v,%v", record, err)
	}
	if warm, err := offline.Lookup(t.Context(), request); err != nil || !warm.Available() || warm.Identity().RepositoryID() != knownRepositoryID(t, 42) {
		t.Fatalf("offline record with ID=%v,%v", warm, err)
	}
	if err := os.WriteFile(path, []byte(cacheRecordJSON(2, "", actionSource, digest(actionSource))), 0600); err != nil {
		t.Fatal(err)
	}
	warm, err := offline.Lookup(t.Context(), request)
	if err != nil || !warm.Available() || !warm.Identity().RepositoryID().IsZero() || warm.Content() != actionSource {
		t.Fatalf("offline record without ID=%v,%v", warm, err)
	}
	before := len(hosts.recorded())
	hit, err := online.Lookup(t.Context(), request)
	if err != nil || !hit.Available() || !hit.Identity().RepositoryID().IsZero() || len(hosts.recorded()) != before {
		t.Fatalf("online record without ID=%v,%v requests=%v", hit, err, hosts.recorded()[before:])
	}
	if err := os.WriteFile(path, []byte(cacheRecordJSON(2, `"repository_id":99,`, actionSource, digest(actionSource))), 0600); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := online.cached(t.Context(), request, knownRepositoryID(t, 42), sha, []string{"action.yml"}); exists || err == nil {
		t.Fatalf("REST fallback accepted a record with a conflicting ID: exists=%t error=%v", exists, err)
	}
	if record, exists, err := online.cached(t.Context(), request, knownRepositoryID(t, 99), sha, []string{"action.yml"}); !exists || err != nil || record.RepositoryID != knownRepositoryID(t, 99) {
		t.Fatalf("REST fallback rejected a record with the same ID: exists=%t error=%v", exists, err)
	}
}

func TestResolverWithoutRepositoryIDUsesLearnedID(t *testing.T) {
	t.Parallel()
	client, err := New(Options{Mode: Online, Resolver: func() references.Resolver {
		return resolveFunc(func(ctx context.Context, r references.Request) (references.Result, error) {
			if r.ExpectedRepositoryID != knownRepositoryID(t, 42) || refListingFromContext(ctx) != nil {
				t.Fatalf("fallback resolution without the learned ID: %+v", r)
			}
			return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-10-31T00:00:00Z", FetchLog: json.RawMessage(`{"profile":"GithubAction"}`)}, nil
		})
	}, Transport: transport(func(r *http.Request) (*http.Response, error) { return restFallback(t, r) })})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
	if err != nil || !result.Available() || result.Identity().RepositoryID() != knownRepositoryID(t, 42) {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestRESTFallbackValidatesRepositoryCommitAndManifest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spec, repository, commit string
		yaml, cached                   bool
		want                           availability.Reason
		requests                       []string
	}{
		{name: "yaml", spec: "example/action@" + sha, repository: `{"id":42,"full_name":"example/action"}`, commit: `{"sha":"` + sha + `"}`, yaml: true, requests: []string{"raw.githubusercontent.com/example/action/" + sha + "/action.yml", "api.github.com/repos/example/action", "api.github.com/repos/example/action/git/commits/" + sha, "api.github.com/repos/example/action/contents/action.yml?ref=" + sha, "api.github.com/repos/example/action/contents/action.yaml?ref=" + sha, "api.github.com/repos/example/action"}},
		{name: "zero ID", spec: "example/action@" + sha, repository: `{"id":0,"full_name":"example/action"}`, want: availability.FailedReason, requests: []string{"raw.githubusercontent.com/example/action/" + sha + "/action.yml", "api.github.com/repos/example/action"}},
		{name: "renamed", spec: "example/action@" + sha, repository: `{"id":42,"full_name":"other/action"}`, want: availability.FailedReason, requests: []string{"raw.githubusercontent.com/example/action/" + sha + "/action.yml", "api.github.com/repos/example/action"}},
		{name: "commit response", spec: "example/action@" + sha, repository: `{"id":42,"full_name":"example/action"}`, commit: `not json`, want: availability.FailedReason, requests: []string{"raw.githubusercontent.com/example/action/" + sha + "/action.yml", "api.github.com/repos/example/action", "api.github.com/repos/example/action/git/commits/" + sha}},
		{name: "cached", spec: "example/action@v1", repository: `{"id":42,"full_name":"example/action"}`, cached: true, requests: []string{"github.com/example/action.git/info/refs?service=git-upload-pack", "api.github.com/repos/example/action"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cache := t.TempDir()
			if tc.cached {
				seeded, err := New(Options{CacheDir: cache})
				if err != nil {
					t.Fatal(err)
				}
				bucket := seeded.bucket(ref(t, "example/action@"+sha), sha)
				if err := os.MkdirAll(bucket, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bucket, cacheFilename), []byte(cacheRecordJSON(2, `"repository_id":42,`, actionSource, digest(actionSource))), 0600); err != nil {
					t.Fatal(err)
				}
			}
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				switch r.URL.Host + r.URL.Path {
				case "github.com/example/action.git/info/refs", "raw.githubusercontent.com/example/action/" + sha + "/action.yml", "cdn.jsdelivr.net/gh/example/action@" + sha + "/action.yml":
					return respond(r, http.StatusServiceUnavailable, "", ""), nil
				case "api.github.com/repos/example/action":
					return respond(r, http.StatusOK, "", tc.repository), nil
				case "api.github.com/repos/example/action/git/commits/" + sha:
					return respond(r, http.StatusOK, "", tc.commit), nil
				case "api.github.com/repos/example/action/contents/action.yml":
					if tc.yaml {
						return respond(r, http.StatusNotFound, "", ""), nil
					}
				case "api.github.com/repos/example/action/contents/action.yaml":
					return respond(r, http.StatusOK, "", actionSource), nil
				}
				t.Fatalf("unexpected request %s", r.URL)
				return nil, nil
			}}
			client, err := New(Options{CacheDir: cache, Transport: hosts, Resolver: func() references.Resolver {
				return resolveFunc(func(context.Context, references.Request) (references.Result, error) {
					return references.Result{Version: 1, Repository: "example/action", Commit: sha, SelectedObject: sha, SelectedRef: "refs/tags/v1", ObservedAt: "2026-11-12T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
				})
			}})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(t.Context(), ref(t, tc.spec))
			if err != nil || manifest.Reason() != tc.want || !slices.Equal(hosts.recorded(), tc.requests) {
				t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
			}
			if tc.want == "" && (manifest.Content() != actionSource || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42)) {
				t.Fatalf("manifest=%v", manifest)
			}
		})
	}
}
