package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/model/availability"
)

func TestWorkflowMetadataUsesWorkflowResolverAndImmutableCache(t *testing.T) {
	cache := t.TempDir()
	const source = "on: workflow_call\npermissions: {contents: write}\njobs: {}\n"
	requests, resolutions := 0, 0
	client, err := New(Options{CacheDir: cache, Resolver: func() references.Resolver {
		return resolveFunc(func(_ context.Context, request references.Request) (references.Result, error) {
			resolutions++
			if request.Kind != "workflow" || request.Ref != "v1" || request.ExpectedRepositoryID != 42 {
				t.Fatalf("workflow used another resolution profile: %+v", request)
			}
			// Workflow resolution does not require an action's selection-route fields.
			return references.Result{Version: 1, Repository: "example/workflows", RepositoryID: 42, Commit: sha, ObservedAt: "2026-11-08T00:00:00Z", FetchLog: json.RawMessage(`{"profile":"GithubWorkflow"}`)}, nil
		})
	}, Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests++
		body := ""
		switch r.URL.Path {
		case "/repos/example/workflows":
			body = `{"id":42,"full_name":"example/workflows"}`
		case "/repos/example/workflows/git/commits/" + sha:
			body = `{"sha":"` + sha + `"}`
		case "/repos/example/workflows/contents/.github/workflows/build.yaml":
			if r.URL.Query().Get("ref") != sha {
				t.Fatal("named ref reached the contents endpoint")
			}
			body = source
		default:
			t.Fatalf("unexpected endpoint %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	file, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yaml@v1")
	if err != nil || !file.Available() || file.Commit() != sha || file.Content() != source || resolutions != 1 || requests != 4 {
		t.Fatalf("file=%+v error=%v resolutions=%d requests=%d", file, err, resolutions, requests)
	}
	warmOnline, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yaml@"+sha)
	if err != nil || !warmOnline.Available() || warmOnline.Content() != source || resolutions != 1 || requests != 4 {
		t.Fatalf("warm online=%+v error=%v resolutions=%d requests=%d", warmOnline, err, resolutions, requests)
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
	for _, scenario := range []string{"identity-changed", "wrong-commit", "forbidden", "missing", "rate-limited", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			repositories := 0
			client, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, "on: workflow_call\njobs: {}\n"
				switch r.URL.Path {
				case "/repos/example/workflows":
					repositories++
					body = `{"id":42,"full_name":"example/workflows"}`
					if scenario == "identity-changed" && repositories > 1 {
						body = `{"id":99,"full_name":"example/workflows"}`
					}
				case "/repos/example/workflows/git/commits/" + sha:
					body = `{"sha":"` + sha + `"}`
					if scenario == "wrong-commit" {
						body = `{"sha":"` + strings.Repeat("b", 40) + `"}`
					}
				case "/repos/example/workflows/contents/.github/workflows/build.yml":
					switch scenario {
					case "forbidden":
						status = http.StatusForbidden
					case "missing":
						status = http.StatusNotFound
					case "rate-limited":
						status = http.StatusTooManyRequests
					case "oversized":
						body = strings.Repeat("x", maxBody+1)
					}
				default:
					t.Fatalf("unexpected endpoint %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			file, err := client.LookupWorkflow(t.Context(), "example/workflows/.github/workflows/build.yml@"+sha)
			if err != nil || file.Available() {
				t.Fatalf("unverified content accepted: %+v %v", file, err)
			}
		})
	}
}
