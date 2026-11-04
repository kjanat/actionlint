package actionhost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/knowledge"
	"actionlint.kjanat.dev/v2/resolver"
)

type resolveFunc func(context.Context, resolver.Request) (resolver.Result, error)

func (f resolveFunc) Resolve(ctx context.Context, r resolver.Request) (resolver.Result, error) {
	return f(ctx, r)
}

func TestNamedMetadataRequiresActionSelectionEvidence(t *testing.T) {
	for _, selectedRef := range []string{"", "refs/heads/v1"} {
		client, err := New(Options{Mode: Online, Resolver: func() resolver.Resolver {
			return resolveFunc(func(_ context.Context, r resolver.Request) (resolver.Result, error) {
				if r.Kind != "action" || r.ExpectedRepositoryID != 42 {
					t.Fatalf("wrong profile or guard: %+v", r)
				}
				return resolver.Result{Version: 1, Repository: "example/action", RepositoryID: 42, Commit: sha, SelectedObject: sha, SelectedRef: selectedRef, ObservedAt: "2026-10-31T00:00:00Z", FetchLog: json.RawMessage(`{"profile":"GithubAction"}`)}, nil
			})
		}, Transport: transport(func(r *http.Request) (*http.Response, error) {
			body := actionSource
			switch r.URL.Path {
			case "/repos/example/action":
				body = `{"id":42,"full_name":"example/action"}`
			case "/repos/example/action/git/commits/" + sha:
				body = `{"sha":"` + sha + `"}`
			case "/repos/example/action/contents/action.yml":
			default:
				t.Fatalf("unexpected endpoint %s", r.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.Lookup(t.Context(), ref(t, "example/action@v1"))
		if err != nil {
			t.Fatal(err)
		}
		if selectedRef == "" {
			if result.Reason() != knowledge.UnsupportedReason {
				t.Fatalf("guessed route: %v", result)
			}
		} else if !result.Available() || result.Selection().Route().Reference() != selectedRef {
			t.Fatalf("lost route: %v", result)
		}
	}
}

func TestMissingObjectNeverFallsBackToNamedTag(t *testing.T) {
	requests := 0
	client, err := New(Options{Mode: Online, Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests++
		status, body := http.StatusOK, `{"id":42,"full_name":"example/action"}`
		if r.URL.Path != "/repos/example/action" {
			if r.URL.Path != "/repos/example/action/git/commits/"+sha {
				t.Fatalf("object fallback: %s", r.URL)
			}
			status = http.StatusNotFound
			body = ""
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
	if err != nil || m.Reason() != knowledge.InaccessibleReason || requests != 2 {
		t.Fatalf("missing object=%v,%v requests=%d", m, err, requests)
	}
}

func TestManifestFallbackLimitAndIdentityGuard(t *testing.T) {
	for _, scenario := range []string{"yaml", "oversized", "identity"} {
		t.Run(scenario, func(t *testing.T) {
			repoReads := 0
			client, err := New(Options{Mode: Online, Transport: transport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusOK, actionSource
				switch r.URL.Path {
				case "/repos/example/action":
					repoReads++
					body = `{"id":42,"full_name":"example/action"}`
					if scenario == "identity" && repoReads > 1 {
						body = `{"id":99,"full_name":"example/action"}`
					}
				case "/repos/example/action/git/commits/" + sha:
					body = `{"sha":"` + sha + `"}`
				case "/repos/example/action/contents/action.yml":
					switch scenario {
					case "yaml":
						status = http.StatusNotFound
					case "oversized":
						body = strings.Repeat("x", maxBody+1)
					}
				case "/repos/example/action/contents/action.yaml":
					if scenario != "yaml" {
						t.Fatal("unexpected fallback")
					}
				default:
					t.Fatalf("unexpected request %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			m, err := client.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "yaml" {
				if !m.Available() || m.Filename() != "action.yaml" {
					t.Fatalf("yaml=%v", m)
				}
			} else if m.Available() {
				t.Fatalf("unsafe availability for %s", scenario)
			}
		})
	}
}

func TestOfflineRejectsCorruptionAndAmbiguousRepositoryIdentity(t *testing.T) {
	client, err := New(Options{Mode: Offline, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	request := ref(t, "example/action@"+sha)
	bucket := client.bucket(request, sha)
	if err := os.MkdirAll(bucket, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "42.json"), []byte(`{"version":1,"content":"tampered"}`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := client.Lookup(t.Context(), request)
	if err != nil || result.Reason() != knowledge.FailedReason {
		t.Fatalf("corrupt=%v,%v", result, err)
	}
	if err := os.WriteFile(filepath.Join(bucket, "99.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = client.Lookup(t.Context(), request)
	if err != nil || result.Reason() != knowledge.FailedReason {
		t.Fatalf("ambiguous=%v,%v", result, err)
	}
}
