package github

import (
	"net/http"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
)

func TestPolicyExcludesRepositoriesFromMirrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, pattern string
		wantMirror    int
	}{
		{"matching repository", "github.com/example/*", 0},
		{"case insensitive", "GitHub.com/EXAMPLE/Action", 0},
		{"other repository", "github.com/other/*", 1},
		{"malformed pattern", "[", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hosts := mirrorHosts(t, reply(http.StatusServiceUnavailable, rawText, ""), reply(http.StatusOK, jsDelivrYAML, actionSource))
			client, err := New(Options{Transport: hosts})
			if err != nil {
				t.Fatal(err)
			}
			exclusions := []string{tc.pattern}
			provider := client.WithMetadataMirrors(references.AllowUnknownMirrors, exclusions)
			exclusions[0] = "github.com/changed/*"
			result, err := provider.Lookup(t.Context(), ref(t, "example/action@"+sha))
			if err != nil || !result.Available() {
				t.Fatalf("lookup = %v, %v", result, err)
			}
			wantREST := 0
			if tc.wantMirror == 0 {
				wantREST = len(restRequests)
			}
			if hosts.count("cdn.jsdelivr.net") != tc.wantMirror || hosts.count("api.github.com") != wantREST {
				t.Fatalf("requests = %v, want %d CDN and %d REST", hosts.recorded(), tc.wantMirror, wantREST)
			}
		})
	}
}

func TestPolicyExcludesWorkflowRepositoryFromMirrors(t *testing.T) {
	t.Parallel()
	const path = "/.github/workflows/build.yml"
	const source = "on: workflow_call\njobs: {}\n"
	hosts := &recorder{handle: func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host + r.URL.Path {
		case "raw.githubusercontent.com/example/workflows/" + sha + path:
			return respond(r, http.StatusServiceUnavailable, "", ""), nil
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
	client, err := New(Options{Transport: hosts})
	if err != nil {
		t.Fatal(err)
	}
	provider := client.WithWorkflowMetadataMirrors(references.AllowUnknownMirrors, []string{"github.com/example/*"})
	result, err := provider.LookupWorkflow(t.Context(), "example/workflows"+path+"@"+sha)
	if err != nil || !result.Available() || result.Content() != source {
		t.Fatalf("lookup = %+v, %v", result, err)
	}
	if hosts.count("cdn.jsdelivr.net") != 0 || hosts.count("api.github.com") != 4 {
		t.Fatalf("requests = %v, want zero CDN and four REST requests", hosts.recorded())
	}
}
