package checking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/integrations/github"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"
)

type resolverMetadataTransport struct {
	mu       sync.Mutex
	requests []string
	commit   string
	tag      string
	missing  bool
}

func (r *resolverMetadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "api.github.com" {
		return nil, fmt.Errorf("unexpected request authority %s", request.URL.Host)
	}
	r.mu.Lock()
	r.requests = append(r.requests, request.URL.RequestURI())
	r.mu.Unlock()
	status, body := http.StatusOK, ""
	if r.missing {
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	}
	switch request.URL.Path {
	case "/rate_limit":
		body = fmt.Sprintf(`{"resources":{"core":{"limit":5000,"remaining":4999,"reset":%d}}}`, time.Now().Add(time.Hour).Unix())
	case "/repos/example/action":
		body = `{"id":42,"full_name":"example/action","default_branch":"master"}`
	case "/repos/example/action/contents":
		body = `[]`
	case "/repos/example/action/git/ref/tags/v1":
		body = fmt.Sprintf(`{"ref":"refs/tags/v1","object":{"sha":%q,"type":"tag"}}`, r.tag)
	case "/repos/example/action/git/ref/heads/v1":
		body = fmt.Sprintf(`{"ref":"refs/heads/v1","object":{"sha":%q,"type":"commit"}}`, strings.Repeat("c", 40))
	case "/repos/example/action/git/commits/" + r.commit:
		body = fmt.Sprintf(`{"sha":%q}`, r.commit)
	case "/repos/example/action/git/tags/" + r.tag:
		body = fmt.Sprintf(`{"sha":%q,"object":{"sha":%q,"type":"commit"}}`, r.tag, r.commit)
	case "/repos/example/action/contents/action.yml":
		if request.URL.Query().Get("ref") != r.commit {
			return nil, fmt.Errorf("metadata requested at wrong commit: %s", request.URL.RawQuery)
		}
		body = "name: test\ndescription: test\ninputs:\n  token: {description: token}\nruns: {using: composite, steps: []}\n"
	default:
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func TestBundledCommitishResolvesMetadataWithoutExternalExecutable(t *testing.T) {
	for _, reference := range []string{"v1", strings.Repeat("b", 40)} {
		t.Run(reference, func(t *testing.T) {
			transport := &resolverMetadataTransport{commit: strings.Repeat("a", 40), tag: strings.Repeat("b", 40)}
			provider, err := github.New(github.Options{Mode: github.Online, CacheDir: t.TempDir(), Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			workflow := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@%s\n        with: {unlisted-input: true}\n", reference)
			result, err := CheckWithPlugins(t.Context(), Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: provider}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "workflow.yml", Reader: strings.NewReader(workflow)}, plugininstall.Sources{})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "remote-resolution" {
					t.Fatalf("metadata lookup unexpectedly enabled commit-pin suggestions: %+v", diagnostic)
				}
				if diagnostic.Rule == "action" && strings.Contains(diagnostic.Message, "unlisted-input") {
					found = true
				}
			}
			if !found {
				t.Fatalf("resolved metadata did not validate action inputs: %+v", result.Diagnostics)
			}
			transport.mu.Lock()
			defer transport.mu.Unlock()
			tagRead, manifestRead, refReads := false, false, 0
			for _, request := range transport.requests {
				tagRead = tagRead || strings.HasPrefix(request, "/repos/example/action/git/tags/"+transport.tag)
				manifestRead = manifestRead || strings.HasPrefix(request, "/repos/example/action/contents/action.yml?ref="+transport.commit)
				if request == "/repos/example/action/git/ref/tags/v1" {
					refReads++
				}
			}
			if !tagRead || !manifestRead {
				t.Fatalf("expected tag peeling and metadata at its commit, got %v", transport.requests)
			}
			if reference == "v1" && refReads != 2 {
				t.Fatalf("expected one reference lookup and one recheck, got %d: %v", refReads, transport.requests)
			}
		})
	}
}

type forbiddenMetadataResolver struct{ t *testing.T }

func (r forbiddenMetadataResolver) Resolve(context.Context, references.Request) (references.Result, error) {
	r.t.Error("offline metadata invoked a resolver")
	return references.Result{}, errors.New("unexpected resolver call")
}

func TestBundledCommitishUnavailableMetadataDoesNotFailAnalysis(t *testing.T) {
	transport := &resolverMetadataTransport{missing: true}
	provider, err := github.New(github.Options{Mode: github.Online, CacheDir: t.TempDir(), Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CheckWithPlugins(t.Context(), Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: provider}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "workflow.yml", Reader: strings.NewReader("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/missing@v1\n")}, plugininstall.Sources{})
	if err != nil {
		t.Fatalf("unavailable optional metadata failed analysis: %v", err)
	}
}

func TestBundledCommitishOfflineDoesNotResolve(t *testing.T) {
	for _, mode := range []github.Mode{github.Off, github.Offline} {
		t.Run(string(mode), func(t *testing.T) {
			transport := &resolverMetadataTransport{}
			provider, err := github.New(github.Options{Mode: mode, CacheDir: t.TempDir(), Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			_, err = CheckWithPlugins(t.Context(), Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: provider, Resolver: forbiddenMetadataResolver{t}, SuggestCommitPins: true}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "workflow.yml", Reader: strings.NewReader("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/action@v1\n")}, plugininstall.Sources{})
			if err != nil {
				t.Fatal(err)
			}
			if len(transport.requests) != 0 {
				t.Fatalf("%s contacted network: %v", mode, transport.requests)
			}
		})
	}
}
