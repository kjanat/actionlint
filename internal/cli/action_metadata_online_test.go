package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/render"
)

type actionMetadataTransport func(*http.Request) (*http.Response, error)

func (f actionMetadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestActionMetadataCLIOnlineThenOfflineValidatesUnknownSHA(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("GH_TOKEN", "fixture-secret")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	requests := 0
	original := http.DefaultTransport
	http.DefaultTransport = actionMetadataTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Fatal("explicit online host credential not forwarded to GitHub")
		}
		body := ""
		switch r.URL.Path {
		case "/repos/example/action":
			body = `{"id":42,"full_name":"example/action"}`
		case "/repos/example/action/git/commits/" + sha:
			body = `{"sha":"` + sha + `"}`
		case "/repos/example/action/contents/action.yml":
			body = "name: test\ndescription: test\ninputs: {known: {description: known}}\nruns: {using: composite, steps: []}\n"
		default:
			t.Fatalf("unexpected HTTP path %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, mode := range []string{"online", "offline"} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@" + sha + ", with: {unknown: value}}]}}\n"), Stdout: &out, Stderr: &log}
		code := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--action-metadata=" + mode, "--output-format=json", "--summary=false", "-"})
		if code != 1 {
			t.Fatalf("%s status=%d stderr=%s output=%s", mode, code, log.String(), out.String())
		}
		var result render.CheckResult
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Commit != sha || result.ActionMetadata[0].State != "available" {
			t.Fatalf("%s coverage=%v", mode, result.ActionMetadata)
		}
		found := false
		for _, d := range result.Diagnostics {
			if d.Rule == "action" && strings.Contains(d.Message, `input "unknown" is not defined`) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s unknown-SHA metadata not used: %v", mode, result.Diagnostics)
		}
		if strings.Contains(out.String()+log.String(), "fixture-secret") {
			t.Fatal("credential leaked to diagnostics")
		}
	}
	if requests != 4 {
		t.Fatalf("warm offline used network: requests=%d", requests)
	}
}
