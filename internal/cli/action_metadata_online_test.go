package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
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
	requests := []string{}
	original := http.DefaultTransport
	http.DefaultTransport = actionMetadataFixture(t, sha, "fixture-secret", &requests)
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, mode := range []string{"default", "online", "offline"} {
		before := len(requests)
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@" + sha + ", with: {unknown: value}}]}}\n"), Stdout: &out, Stderr: &log}
		args := []string{"actionlint", "check", "--no-config", "--output-format=json", "--summary=false", "--network-stats", "-"}
		if mode != "default" {
			args = append(args, "--action-metadata="+mode)
		}
		code := command.MainContext(t.Context(), args)
		if code != 1 {
			t.Fatalf("%s status=%d stderr=%s output=%s", mode, code, log.String(), out.String())
		}
		var result resultreport.CheckResult
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
		var statistics struct {
			Network accounting.Statistics `json:"network"`
		}
		if err := json.Unmarshal(log.Bytes(), &statistics); err != nil {
			t.Fatal(err)
		}
		var attempts, resolverCalls, unreported uint64
		for _, group := range statistics.Network.Requests {
			attempts += group.Attempts
			resolverCalls += group.ResolverCalls
			unreported += group.UnreportedResolverCalls
			if group.Attempts != 0 && (group.Host != "api.github.com" || group.Credentials != accounting.CredentialsProvided || group.RequestKind != accounting.REST) {
				t.Fatalf("wrong accounting: %+v", group)
			}
		}
		want := map[string]int{}
		if mode == "default" {
			want = map[string]int{"/rate_limit": 1, "/repos/example/action": 4, "/repos/example/action/contents?ref=main": 1, "/repos/example/action/git/commits/" + sha: 2, "/repos/example/action/contents/action.yml?ref=" + sha: 1}
		}
		assertMetadataRequests(t, requests[before:], want)
		if attempts != uint64(len(requests)-before) {
			t.Fatalf("%s attempts=%d, actual requests=%d", mode, attempts, len(requests)-before)
		}
		wantCalls := uint64(0)
		if mode == "default" {
			wantCalls = 1
		}
		if resolverCalls != wantCalls || unreported != 0 {
			t.Fatalf("%s resolver calls=%d, unreported=%d", mode, resolverCalls, unreported)
		}
	}
}

// actionMetadataFixture uses fixed API paths and records every request, including rechecks.
func actionMetadataFixture(t *testing.T, sha, token string, requests *[]string) actionMetadataTransport {
	t.Helper()
	return func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
			t.Fatalf("unexpected HTTP authority %s", r.URL)
		}
		wantAuth := ""
		if token != "" {
			wantAuth = "Bearer " + token
		}
		if r.Header.Get("Authorization") != wantAuth {
			t.Fatal("unexpected GitHub authorization header")
		}
		*requests = append(*requests, r.URL.RequestURI())
		status, body := http.StatusOK, ""
		switch r.URL.Path {
		case "/rate_limit":
			body = fmt.Sprintf(`{"resources":{"core":{"remaining":100,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
		case "/repos/example/action":
			body = `{"id":42,"full_name":"example/action","default_branch":"main"}`
		case "/repos/example/action/contents":
			if r.URL.Query().Get("ref") != "main" {
				t.Fatalf("wrong contents guard: %s", r.URL)
			}
			body = `[]`
		case "/repos/example/action/git/ref/v1":
			status = http.StatusNotFound
		case "/repos/example/action/git/ref/tags/v1":
			body = fmt.Sprintf(`{"ref":"refs/tags/v1","object":{"sha":%q,"type":"commit"}}`, sha)
		case "/repos/example/action/git/commits/" + sha:
			body = fmt.Sprintf(`{"sha":%q}`, sha)
		case "/repos/example/action/contents/action.yml":
			if r.URL.Query().Get("ref") != sha {
				t.Fatalf("wrong metadata commit: %s", r.URL)
			}
			body = "name: test\ndescription: test\ninputs: {known: {description: known}}\nruns: {using: composite, steps: []}\n"
		default:
			t.Fatalf("unexpected HTTP path %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
}

func assertMetadataRequests(t *testing.T, requests []string, want map[string]int) {
	t.Helper()
	got := make(map[string]int)
	for _, request := range requests {
		got[request]++
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
}

func TestActionMetadataCLIBundledResolverHandlesNamedRef(t *testing.T) {
	cache := t.TempDir()
	for _, name := range []string{"LOCALAPPDATA", "XDG_CACHE_HOME", "HOME"} {
		t.Setenv(name, cache)
	}
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	var requests []string
	original := http.DefaultTransport
	http.DefaultTransport = actionMetadataFixture(t, sha, "", &requests)
	t.Cleanup(func() { http.DefaultTransport = original })
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader("on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1, with: {unknown: value}}]}}\n"), Stdout: &out, Stderr: &log}
	code := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-config", "--output=json", "--summary=false", "--network-stats", "-"})
	if code != 1 {
		t.Fatalf("status=%d stderr=%s stdout=%s", code, &log, &out)
	}
	var result resultreport.CheckResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ActionMetadata) != 1 || result.ActionMetadata[0].State != "available" || result.ActionMetadata[0].Commit != sha {
		t.Fatalf("coverage=%+v", result.ActionMetadata)
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Rule == "action" && strings.Contains(d.Message, `input "unknown" is not defined`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("named-ref metadata not used: %v", result.Diagnostics)
	}
	assertMetadataRequests(t, requests, map[string]int{"/rate_limit": 1, "/repos/example/action": 4, "/repos/example/action/contents?ref=main": 3, "/repos/example/action/git/ref/v1": 2, "/repos/example/action/git/ref/tags/v1": 2, "/repos/example/action/git/commits/" + sha: 2, "/repos/example/action/contents/action.yml?ref=" + sha: 1})
	var statistics struct {
		Network accounting.Statistics `json:"network"`
	}
	if err := json.Unmarshal(log.Bytes(), &statistics); err != nil {
		t.Fatal(err)
	}
	var attempts, failures uint64
	for _, group := range statistics.Network.Requests {
		if group.Purpose != "resolver" || group.Attempts == 0 {
			continue
		}
		if group.Host != "api.github.com" || group.Credentials != accounting.NoCredentials || group.RequestKind != accounting.REST {
			t.Fatalf("wrong resolver accounting: %+v", group)
		}
		attempts += group.Attempts
		failures += group.Failures
	}
	if attempts != 10 || failures != 2 {
		t.Fatalf("resolver attempts=%d failures=%d, want 10 attempts and 2 failed 404 responses", attempts, failures)
	}
}
