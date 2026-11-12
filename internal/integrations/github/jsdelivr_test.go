package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

type handler = func(*http.Request) (*http.Response, error)

func reply(status int, contentType, body string) handler {
	return func(r *http.Request) (*http.Response, error) { return respond(r, status, contentType, body), nil }
}

func rawRequestURI(commit, file string) string {
	return "raw.githubusercontent.com/example/action/" + commit + "/" + file
}

func jsDelivrRequestURI(commit, file string) string {
	return "cdn.jsdelivr.net/gh/example/action@" + commit + "/" + file
}

var restRequests = []string{
	"api.github.com/repos/example/action",
	"api.github.com/repos/example/action/git/commits/" + sha,
	"api.github.com/repos/example/action/contents/action.yml?ref=" + sha,
	"api.github.com/repos/example/action",
}

func mirrorHosts(t *testing.T, raw, cdn handler) *recorder {
	t.Helper()
	return &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "raw.githubusercontent.com":
			return raw(r)
		case "cdn.jsdelivr.net":
			return cdn(r)
		case "github.com":
			return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
		}
		return restFallback(t, r)
	}}
}

func requestsTo(hosts *recorder, host string) []string {
	var matching []string
	for _, request := range hosts.recorded() {
		if strings.HasPrefix(request, host+"/") {
			matching = append(matching, request)
		}
	}
	return matching
}

func TestRawNotFoundNeverReachesJSDelivr(t *testing.T) {
	for _, tc := range []struct {
		name, spec, token string
		raw               int
		listing           bool
	}{
		{name: "full SHA", spec: "example/action@" + sha, raw: 2},
		{name: "full SHA after anonymous retry", spec: "example/action@" + sha, token: "fixture-token", raw: 4},
		{name: "named reference", spec: "example/action@v2", token: "fixture-token", raw: 4, listing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				switch r.URL.Host {
				case "raw.githubusercontent.com":
					return respond(r, http.StatusNotFound, rawText, "404: Not Found"), nil
				case "github.com":
					if tc.listing {
						return respond(r, http.StatusOK, gitwire.ContentType, actionListing()), nil
					}
				}
				t.Fatalf("raw 404 reached %s", r.URL)
				return nil, nil
			}}
			client := newListingClient(t, Options{Token: tc.token, Transport: hosts}, &listingResolver{})
			if !tc.listing {
				var err error
				if client, err = New(Options{Token: tc.token, Transport: hosts}); err != nil {
					t.Fatal(err)
				}
			}
			manifest, err := client.Lookup(t.Context(), ref(t, tc.spec))
			if err != nil || manifest.Available() || manifest.Reason() != availability.InaccessibleReason || hosts.count("raw.githubusercontent.com") != tc.raw || hosts.count("cdn.jsdelivr.net") != 0 || hosts.count("api.github.com") != 0 {
				t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
			}
		})
	}
}

func TestRawNotFoundBeforeFailureSkipsJSDelivr(t *testing.T) {
	for _, token := range []string{"", "fixture-token"} {
		t.Run("token="+token, func(t *testing.T) {
			requests := 0
			hosts := mirrorHosts(t, func(r *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return respond(r, http.StatusNotFound, rawText, ""), nil
				}
				return respond(r, http.StatusServiceUnavailable, rawText, ""), nil
			}, reply(http.StatusOK, jsDelivrYAML, actionSource))
			client, err := New(Options{Token: token, Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil || !manifest.Available() || hosts.count("cdn.jsdelivr.net") != 0 || hosts.count("api.github.com") == 0 {
				t.Fatalf("lookup=%v,%v; after a raw 404 only GitHub may be contacted, requests=%v", manifest, err, hosts.recorded())
			}
		})
	}
}

func TestTagObjectNotFoundPreventsCDNFallbackAtItsCommit(t *testing.T) {
	hosts := mirrorHosts(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, tagObject) {
			return respond(r, http.StatusNotFound, rawText, ""), nil
		}
		return respond(r, http.StatusServiceUnavailable, rawText, ""), nil
	}, reply(http.StatusOK, jsDelivrYAML, actionSource))
	client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
	file, err := client.lookupCommitFile(t.Context(), ref(t, "example/action@"+tagObject), "action", []string{"action.yml", "action.yaml"})
	if file.Available() || !errors.Is(err, errEnvironment) || hosts.count("cdn.jsdelivr.net") != 0 || hosts.count("raw.githubusercontent.com") != 3 {
		t.Fatalf("lookup=%v,%v requests=%v", file, err, hosts.recorded())
	}
}

func TestJSDelivrFileIsCachedWithoutRepositoryID(t *testing.T) {
	cache := t.TempDir()
	hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), reply(http.StatusOK, jsDelivrYAML, actionSource))
	client, err := New(Options{Mirrors: references.AllowUnknownMirrors, CacheDir: cache, Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action@"+sha)
	manifest, err := client.Lookup(t.Context(), request)
	want := []string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml")}
	if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Filename() != "action.yml" || manifest.Identity().Commit() != sha || !manifest.Identity().RepositoryID().IsZero() || !slices.Equal(hosts.recorded(), want) {
		t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
	}
	stored, err := os.ReadFile(filepath.Join(client.bucket(request, sha), cacheFilename))
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(stored, &record); err != nil || record["version"] != float64(cacheVersion) || record["filename"] != "action.yml" || record["content"] != actionSource || record["commit"] != sha {
		t.Fatalf("record=%s,%v", stored, err)
	}
	if _, present := record["repository_id"]; present {
		t.Fatalf("jsDelivr acquisition recorded a repository ID: %s", stored)
	}
	warm, err := New(Options{Mirrors: references.AllowUnknownMirrors, CacheDir: cache, Transport: transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("warm cache used the network: %s", r.URL)
		return nil, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := warm.Lookup(t.Context(), request); err != nil || !hit.Available() || hit.Content() != actionSource || !hit.Identity().RepositoryID().IsZero() {
		t.Fatalf("warm=%v,%v", hit, err)
	}
}

func TestJSDelivrNotFoundTriesSecondFilenameThenAPI(t *testing.T) {
	t.Run("action.yaml", func(t *testing.T) {
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/action.yaml") {
				return respond(r, http.StatusOK, jsDelivrYAML, actionSource), nil
			}
			return respond(r, http.StatusNotFound, rawText, "Couldn't find the requested file."), nil
		})
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		want := []string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yaml")}
		if err != nil || !manifest.Available() || manifest.Filename() != "action.yaml" || !manifest.Identity().RepositoryID().IsZero() || !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
		}
	})
	t.Run("neither", func(t *testing.T) {
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), reply(http.StatusNotFound, rawText, "Couldn't find the requested file."))
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		want := append([]string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yaml")}, restRequests...)
		if err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
		}
	})
}

func TestJSDelivrEnvironmentFailuresFallBackToAPI(t *testing.T) {
	failures := append(environmentFailures(),
		failure{name: "oversized", status: http.StatusOK, contentType: jsDelivrYAML, body: strings.Repeat("x", maxBody+1)},
		failure{name: "plain text", status: http.StatusOK, contentType: rawText, body: actionSource},
	)
	for _, f := range failures {
		t.Run(f.name, func(t *testing.T) {
			hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), f.respond)
			client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			want := append([]string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml")}, restRequests...)
			if err != nil || !manifest.Available() || manifest.Content() != actionSource || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || !slices.Equal(hosts.recorded(), want) {
				t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
			}
		})
	}
}

func TestJSDelivrTimeoutFallsBackToAPI(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		want := append([]string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml")}, restRequests...)
		if err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || time.Since(start) != client.client.Timeout || !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("manifest=%v,%v after %s requests=%v", manifest, err, time.Since(start), hosts.recorded())
		}
	})
}

func TestJSDelivrRequestsOnlyResolvedCommits(t *testing.T) {
	cdn := func(r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, jsDelivrYAML, actionSource), nil
	}
	hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), cdn)
	client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
	for _, tc := range []struct{ spec, commit string }{
		{"example/action@v1", sha},
		{"example/action@release", branchTip},
		{"example/action@" + strings.ToUpper(branchTip), branchTip},
	} {
		manifest, err := client.Lookup(t.Context(), ref(t, tc.spec))
		if err != nil || !manifest.Available() || manifest.Identity().Commit() != tc.commit {
			t.Fatalf("%s=%v,%v requests=%v", tc.spec, manifest, err, hosts.recorded())
		}
	}
	want := []string{jsDelivrRequestURI(sha, "action.yml"), jsDelivrRequestURI(branchTip, "action.yml"), jsDelivrRequestURI(branchTip, "action.yml")}
	if got := requestsTo(hosts, "cdn.jsdelivr.net"); !slices.Equal(got, want) || hosts.count("github.com") != 1 {
		t.Fatalf("jsDelivr requests=%v, want %v; all=%v", got, want, hosts.recorded())
	}
	before := len(hosts.recorded())
	if _, _, missing, err := client.fetchRawFile(t.Context(), ref(t, "example/action@main"), "main", "action", []string{"action.yml"}); missing != nil || !errors.Is(err, errEnvironment) {
		t.Fatalf("named commit=%v,%v", missing, err)
	}
	if got := hosts.recorded()[before:]; !slices.Equal(got, []string{"raw.githubusercontent.com/example/action/main/action.yml"}) {
		t.Fatalf("a branch name reached jsDelivr: %v", got)
	}
}

func TestJSDelivrUsesRawRequestLimits(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), func(r *http.Request) (*http.Response, error) {
			response := respond(r, http.StatusFound, "", "")
			response.Header.Set("Location", "https://foreign.test/action.yml")
			return response, nil
		})
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
		want := append([]string{rawRequestURI(sha, "action.yml"), jsDelivrRequestURI(sha, "action.yml")}, restRequests...)
		if err != nil || !manifest.Available() || manifest.Identity().RepositoryID() != knownRepositoryID(t, 42) || !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
		}
	})
	t.Run("transfer slot", func(t *testing.T) {
		var client *GitHub
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), func(r *http.Request) (*http.Response, error) {
			if client.requests.TryAcquire(concurrentTransfers) {
				client.requests.Release(concurrentTransfers)
				t.Error("jsDelivr request holds no transfer slot")
			}
			return respond(r, http.StatusOK, jsDelivrYAML, actionSource), nil
		})
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		if manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha)); err != nil || !manifest.Available() || hosts.count("cdn.jsdelivr.net") != 1 {
			t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
		}
	})
	t.Run("escaping", func(t *testing.T) {
		hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, "", ""), reply(http.StatusOK, jsDelivrYAML, actionSource))
		client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := client.Lookup(t.Context(), ref(t, "example/action/a b/c#d?e@"+sha))
		want := []string{rawRequestURI(sha, "a%20b/c%23d%3Fe/action.yml"), jsDelivrRequestURI(sha, "a%20b/c%23d%3Fe/action.yml")}
		if err != nil || !manifest.Available() || !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("manifest=%v,%v requests=%v", manifest, err, hosts.recorded())
		}
	})
	t.Run("lookup budget", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			hosts := mirrorHosts(t, func(r *http.Request) (*http.Response, error) {
				time.Sleep(10 * time.Second)
				return respond(r, http.StatusServiceUnavailable, "", ""), nil
			}, func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/action.yml") {
					time.Sleep(10 * time.Second)
					return respond(r, http.StatusNotFound, jsDelivrYAML, ""), nil
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			manifest, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil || manifest.Reason() != availability.FailedReason || time.Since(start) != lookupBudget || hosts.count("cdn.jsdelivr.net") != 2 || hosts.count("api.github.com") != 0 {
				t.Fatalf("manifest=%v,%v after %s requests=%v", manifest, err, time.Since(start), hosts.recorded())
			}
		})
	})
}

func TestRequestAccountingLabelsJSDelivr(t *testing.T) {
	ctx, tracker := accounting.TrackRequests(t.Context())
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "raw.githubusercontent.com":
			return respond(r, http.StatusServiceUnavailable, "", ""), nil
		case "cdn.jsdelivr.net":
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("credentials sent to jsDelivr: %s", r.URL)
			}
			return respond(r, http.StatusOK, jsDelivrYAML, "on: workflow_call\njobs: {}\n"), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	}}
	client, err := New(Options{Mirrors: references.AllowUnknownMirrors, Token: "fixture-token", Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Lookup(ctx, ref(t, "example/action@"+sha)); err != nil || hosts.count("cdn.jsdelivr.net") != 1 {
		t.Fatalf("manifest error=%v requests=%v", err, hosts.recorded())
	}
	if workflow, err := client.LookupWorkflow(ctx, "example/action/.github/workflows/build.yml@"+sha); err != nil || !workflow.Available() {
		t.Fatalf("workflow=%+v,%v", workflow, err)
	}
	type group struct {
		host        string
		credentials accounting.CredentialStatus
		kind        accounting.RequestKind
		purpose     string
		attempts    uint64
		failures    uint64
	}
	var groups []group
	for _, counts := range tracker.Statistics().Requests {
		if counts.Priority != accounting.Enrichment {
			t.Fatalf("group=%+v", counts)
		}
		groups = append(groups, group{counts.Host, counts.Credentials, counts.RequestKind, counts.Purpose, counts.Attempts, counts.Failures})
	}
	want := []group{
		{"cdn.jsdelivr.net", accounting.NoCredentials, accounting.HTTP, "action-metadata", 1, 0},
		{"cdn.jsdelivr.net", accounting.NoCredentials, accounting.HTTP, "workflow-metadata", 1, 0},
		{"raw.githubusercontent.com", accounting.CredentialsProvided, accounting.HTTP, "action-metadata", 1, 1},
		{"raw.githubusercontent.com", accounting.CredentialsProvided, accounting.HTTP, "workflow-metadata", 1, 1},
	}
	if !slices.Equal(groups, want) {
		t.Fatalf("groups=%+v, want %+v", groups, want)
	}
}

func TestWorkflowFileUsesJSDelivrAfterRawFailure(t *testing.T) {
	const source = "on: workflow_call\njobs: {}\n"
	const path = "/.github/workflows/build.yml"
	workflowHosts := func(t *testing.T, cdn handler) *recorder {
		t.Helper()
		return &recorder{handle: func(r *http.Request) (*http.Response, error) {
			switch r.URL.Host + r.URL.Path {
			case "github.com/example/workflows.git/info/refs":
				return respond(r, http.StatusOK, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/tags/v1", Object: sha})), nil
			case "raw.githubusercontent.com/example/workflows/" + sha + path:
				return respond(r, http.StatusServiceUnavailable, "", ""), nil
			case "cdn.jsdelivr.net/gh/example/workflows@" + sha + path:
				return cdn(r)
			case "api.github.com/repos/example/workflows":
				return respond(r, http.StatusOK, "", `{"id":42,"full_name":"example/workflows"}`), nil
			case "api.github.com/repos/example/workflows/git/commits/" + sha:
				return respond(r, http.StatusOK, "", `{"sha":"`+sha+`"}`), nil
			case "api.github.com/repos/example/workflows/contents" + path:
				return respond(r, http.StatusOK, "", source), nil
			}
			t.Fatalf("unexpected request %s", r.URL)
			return nil, nil
		}}
	}
	t.Run("found", func(t *testing.T) {
		hosts := workflowHosts(t, reply(http.StatusOK, jsDelivrYAML, source))
		client := newListingClient(t, Options{Transport: hosts}, &listingResolver{})
		for _, spec := range []string{"example/workflows" + path + "@v1", "example/workflows" + path + "@" + sha} {
			file, err := client.LookupWorkflow(t.Context(), spec)
			if err != nil || !file.Available() || file.Commit() != sha || file.Content() != source {
				t.Fatalf("%s=%+v,%v requests=%v", spec, file, err, hosts.recorded())
			}
		}
		want := []string{
			"github.com/example/workflows.git/info/refs?service=git-upload-pack",
			"raw.githubusercontent.com/example/workflows/" + sha + path,
			"cdn.jsdelivr.net/gh/example/workflows@" + sha + path,
			"raw.githubusercontent.com/example/workflows/" + sha + path,
			"cdn.jsdelivr.net/gh/example/workflows@" + sha + path,
		}
		if !slices.Equal(hosts.recorded(), want) {
			t.Fatalf("requests=%v", hosts.recorded())
		}
	})
	t.Run("missing", func(t *testing.T) {
		hosts := workflowHosts(t, reply(http.StatusNotFound, rawText, "Couldn't find the requested file."))
		client, err := New(Options{Transport: hosts, Mirrors: references.AllowUnknownMirrors})
		if err != nil {
			t.Fatal(err)
		}
		file, err := client.LookupWorkflow(t.Context(), "example/workflows"+path+"@"+sha)
		if err != nil || !file.Available() || file.Content() != source || hosts.count("cdn.jsdelivr.net") != 1 || hosts.count("api.github.com") != 4 {
			t.Fatalf("file=%+v,%v requests=%v", file, err, hosts.recorded())
		}
	})
}
