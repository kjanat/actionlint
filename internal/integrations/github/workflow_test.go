package github

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestWorkflowMetadataUsesWorkflowResolverAndImmutableCache(t *testing.T) {
	cache := t.TempDir()
	const source = "on: workflow_call\npermissions: {contents: write}\njobs: {}\n"
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "github.com/example/workflows.git/info/refs":
			return respond(r, http.StatusOK, gitwire.ContentType, listing(sha, gitwire.Ref{Name: "refs/heads/main", Object: sha}, gitwire.Ref{Name: "refs/tags/v1", Object: sha})), nil
		case "raw.githubusercontent.com/example/workflows/" + sha + "/.github/workflows/build.yaml":
			return respond(r, http.StatusOK, rawText, source), nil
		}
		t.Fatalf("unexpected endpoint %s", r.URL)
		return nil, nil
	}}
	resolver := &listingResolver{}
	client := newListingClient(t, Options{CacheDir: cache, Transport: hosts}, resolver)
	file, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yaml@v1")
	if err != nil || !file.Available() || file.Commit() != sha || file.Content() != source || resolver.resolutions() != 1 || len(hosts.recorded()) != 2 || hosts.count("github.com") != 1 {
		t.Fatalf("file=%+v error=%v resolutions=%d requests=%v", file, err, resolver.resolutions(), hosts.recorded())
	}
	offline, err := New(Options{Mode: Offline, CacheDir: cache, Transport: transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("offline request %s", r.URL)
		return nil, context.Canceled
	})})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := offline.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yaml@"+sha)
	if err != nil || !warm.Available() || warm.Content() != source {
		t.Fatalf("warm=%+v error=%v", warm, err)
	}
	for _, spec := range []string{"example/workflows/.github/workflows/other.yaml@" + sha, "other/workflows/.github/workflows/build.yaml@" + sha} {
		miss, err := offline.LookupWorkflow(t.Context(), spec)
		if err != nil || miss.Available() || miss.Reason() != availability.CacheMissReason {
			t.Fatalf("foreign cache hit: %+v %v", miss, err)
		}
	}
	named, err := offline.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yaml@v1")
	if err != nil || named.Available() || named.Reason() != availability.OfflineReason {
		t.Fatalf("offline named ref=%+v %v", named, err)
	}
}

func TestWorkflowMetadataRejectsForeignRepositoryOrCommit(t *testing.T) {
	for _, tc := range []struct {
		scenario  string
		rawStatus int
		api       bool
		want      availability.Reason
	}{
		{"identity-changed", http.StatusServiceUnavailable, true, availability.FailedReason},
		{"wrong-commit", http.StatusServiceUnavailable, true, availability.FailedReason},
		{"forbidden", http.StatusForbidden, true, availability.DeniedReason},
		{"rate-limited", http.StatusTooManyRequests, true, availability.LimitReason},
		{"missing", http.StatusNotFound, false, availability.InaccessibleReason},
		{"oversized", http.StatusOK, false, availability.LimitReason},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			repositories := 0
			hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, "on: workflow_call\njobs: {}\n"
				switch r.URL.Host + r.URL.Path {
				case "raw.githubusercontent.com/example/workflows/" + sha + "/.github/workflows/build.yml":
					if tc.scenario == "oversized" {
						body = strings.Repeat("x", maxBody+1)
					}
					return respond(r, tc.rawStatus, rawText, body), nil
				case "cdn.jsdelivr.net/gh/example/workflows@" + sha + "/.github/workflows/build.yml":
					return respond(r, http.StatusServiceUnavailable, "", ""), nil
				case "api.github.com/repos/example/workflows":
					repositories++
					body = `{"id":42,"full_name":"example/workflows"}`
					if tc.scenario == "identity-changed" && repositories > 1 {
						body = `{"id":99,"full_name":"example/workflows"}`
					}
				case "api.github.com/repos/example/workflows/git/commits/" + sha:
					body = `{"sha":"` + sha + `"}`
					if tc.scenario == "wrong-commit" {
						body = `{"sha":"` + strings.Repeat("b", 40) + `"}`
					}
				case "api.github.com/repos/example/workflows/contents/.github/workflows/build.yml":
					switch tc.scenario {
					case "forbidden":
						status = http.StatusForbidden
					case "rate-limited":
						status = http.StatusTooManyRequests
					}
				default:
					t.Fatalf("unexpected endpoint %s", r.URL)
				}
				return respond(r, status, "", body), nil
			}}
			client, err := New(Options{Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			file, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yml@"+sha)
			if err != nil || file.Available() || file.Reason() != tc.want || (hosts.count("api.github.com") != 0) != tc.api || (hosts.count("cdn.jsdelivr.net") != 0) != tc.api {
				t.Fatalf("unverified content accepted: %+v %v requests=%v", file, err, hosts.recorded())
			}
		})
	}
}

func TestWorkflowUppercaseCommitIsRejectedBeforeNetwork(t *testing.T) {
	client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("uppercase workflow commit reached the network: %s", r.URL)
		return nil, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	file, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yml@"+strings.Repeat("A", 40))
	if err != nil || file.Available() || file.Reason() != availability.FailedReason {
		t.Fatalf("file=%+v error=%v", file, err)
	}
}
