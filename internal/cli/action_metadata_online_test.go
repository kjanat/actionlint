package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"

	acquisitionhttp "actionlint.kjanat.dev/v2/internal/integrations/http"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

type actionMetadataTransport func(*http.Request) (*http.Response, error)

func (f actionMetadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestActionMetadataCLIOnlineThenOfflineValidatesUnknownSHA(t *testing.T) {
	t.Setenv("GH_TOKEN", "fixture-secret")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	requests := []string{}
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataFixture(t, sha, "fixture-secret", &requests)}}
	for _, mode := range []string{"default", "online", "offline"} {
		before := len(requests)
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@" + sha + ", with: {unknown: value}}]}}\n"), Stdout: &out, Stderr: &log, Acquisition: acquisition}
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
			if group.Attempts != 0 && (group.Host != "raw.githubusercontent.com" || group.Credentials != accounting.CredentialsProvided || group.RequestKind != accounting.HTTP || group.Purpose != "action-metadata") {
				t.Fatalf("wrong accounting: %+v", group)
			}
		}
		want := map[string]int{}
		if mode == "default" {
			want = map[string]int{"raw.githubusercontent.com/example/action/" + sha + "/action.yml": 1}
		}
		assertMetadataRequests(t, requests[before:], want)
		if attempts != uint64(len(requests)-before) {
			t.Fatalf("%s attempts=%d, actual requests=%d", mode, attempts, len(requests)-before)
		}
		if resolverCalls != 0 || unreported != 0 {
			t.Fatalf("%s resolver calls=%d, unreported=%d", mode, resolverCalls, unreported)
		}
	}
}

// actionMetadataFixture serves the Git reference listing and raw files of example/action.
func actionMetadataFixture(t *testing.T, sha, token string, requests *[]string) actionMetadataTransport {
	t.Helper()
	return func(r *http.Request) (*http.Response, error) {
		t.Helper()
		if r.URL.Scheme != "https" {
			t.Fatalf("unexpected HTTP scheme %s", r.URL)
		}
		*requests = append(*requests, r.URL.Host+r.URL.RequestURI())
		header := http.Header{}
		switch r.URL.Host {
		case "github.com":
			wantAuth := ""
			if token != "" {
				wantAuth = "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
			}
			if r.Header.Get("Authorization") != wantAuth || r.URL.Path != "/example/action.git/info/refs" || r.URL.RawQuery != "service=git-upload-pack" {
				t.Fatalf("unexpected Git request %s", r.URL)
			}
			header.Set("Content-Type", gitwire.ContentType)
			body := gitwire.RefAdvertisement(sha, "refs/heads/main", gitwire.Ref{Name: "refs/heads/main", Object: sha}, gitwire.Ref{Name: "refs/tags/v1", Object: sha})
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
		case "raw.githubusercontent.com":
			wantAuth := ""
			if token != "" {
				wantAuth = "Bearer " + token
			}
			if r.Header.Get("Authorization") != wantAuth || r.URL.Path != "/example/action/"+sha+"/action.yml" {
				t.Fatalf("unexpected raw file request %s", r.URL)
			}
			header.Set("Content-Type", "text/plain; charset=utf-8")
			body := "name: test\ndescription: test\ninputs: {known: {description: known}}\nruns: {using: composite, steps: []}\n"
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		t.Fatalf("unexpected HTTP authority %s", r.URL)
		return nil, nil
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
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	sha := strings.Repeat("a", 40)
	var requests []string
	acquisition := plugininstall.Acquisition{UserCacheDir: t.TempDir(), Connections: acquisitionhttp.Connections{Transport: actionMetadataFixture(t, sha, "", &requests)}}
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader("on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: example/action@v1, with: {unknown: value}}]}}\n"), Stdout: &out, Stderr: &log, Acquisition: acquisition}
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
	assertMetadataRequests(t, requests, map[string]int{"github.com/example/action.git/info/refs?service=git-upload-pack": 1, "raw.githubusercontent.com/example/action/" + sha + "/action.yml": 1})
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
		if group.Host != "github.com" || group.Credentials != accounting.NoCredentials || group.RequestKind != accounting.Git {
			t.Fatalf("wrong resolver accounting: %+v", group)
		}
		attempts += group.Attempts
		failures += group.Failures
	}
	if attempts != 1 || failures != 0 {
		t.Fatalf("resolver attempts=%d failures=%d, want one reference listing", attempts, failures)
	}
}
