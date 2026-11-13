package checking

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
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
	r.mu.Lock()
	r.requests = append(r.requests, request.URL.Host+request.URL.RequestURI())
	r.mu.Unlock()
	header := http.Header{}
	switch request.URL.Host {
	case "github.com":
		if r.missing || request.URL.Path != "/example/action.git/info/refs" {
			return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
		}
		header.Set("Content-Type", gitwire.ContentType)
		body := gitwire.RefAdvertisement(r.commit, "refs/heads/master",
			gitwire.Ref{Name: "refs/heads/master", Object: r.commit},
			gitwire.Ref{Name: "refs/heads/v1", Object: strings.Repeat("c", 40)},
			gitwire.Ref{Name: "refs/tags/v1", Object: r.tag, Peeled: r.commit},
		)
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	case "raw.githubusercontent.com":
		header.Set("Content-Type", "text/plain; charset=utf-8")
		if r.missing || request.URL.Path != "/example/action/"+r.commit+"/action.yml" {
			return &http.Response{StatusCode: http.StatusNotFound, Header: header, Body: io.NopCloser(strings.NewReader("404: Not Found")), Request: request}, nil
		}
		body := "name: test\ndescription: test\ninputs:\n  token: {description: token}\nruns: {using: composite, steps: []}\n"
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	}
	return nil, fmt.Errorf("unexpected request authority %s", request.URL.Host)
}

func TestBundledCommitishResolvesMetadataWithoutExternalExecutable(t *testing.T) {
	for _, reference := range []string{"v1", strings.Repeat("a", 40), strings.Repeat("b", 40)} {
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
			transport.mu.Lock()
			defer transport.mu.Unlock()
			manifest := "raw.githubusercontent.com/example/action/" + transport.commit + "/action.yml"
			switch reference {
			case "v1":
				want := []string{"github.com/example/action.git/info/refs?service=git-upload-pack", manifest}
				if !found || !slices.Equal(transport.requests, want) {
					t.Fatalf("found=%t requests=%v, want %v", found, transport.requests, want)
				}
			case transport.commit:
				if !found || !slices.Equal(transport.requests, []string{manifest}) {
					t.Fatalf("found=%t requests=%v", found, transport.requests)
				}
			case transport.tag:
				want := []string{"raw.githubusercontent.com/example/action/" + transport.tag + "/action.yml", "raw.githubusercontent.com/example/action/" + transport.tag + "/action.yaml", "github.com/example/action.git/info/refs?service=git-upload-pack", manifest}
				if !found || !slices.Equal(transport.requests, want) || len(result.ActionMetadata) != 1 || result.ActionMetadata[0].Availability.State() != actions.MetadataAvailable {
					t.Fatalf("tag object=%t requests=%v coverage=%+v", found, transport.requests, result.ActionMetadata)
				}
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

type barrierTransport struct {
	host     string
	mu       sync.Mutex
	inFlight int
	released chan struct{}
	commit   string
}

func (b *barrierTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	header := http.Header{}
	if request.URL.Host == b.host {
		b.mu.Lock()
		b.inFlight++
		if b.inFlight == 2 {
			close(b.released)
		}
		b.mu.Unlock()
		select {
		case <-b.released:
		case <-time.After(5 * time.Second):
			return nil, errors.New("metadata requests for different repositories ran one after another")
		}
	}
	switch request.URL.Host {
	case "github.com":
		header.Set("Content-Type", gitwire.ContentType)
		body := gitwire.RefAdvertisement(b.commit, "refs/heads/main", gitwire.Ref{Name: "refs/tags/v1", Object: b.commit})
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	case "raw.githubusercontent.com":
		header.Set("Content-Type", "text/plain; charset=utf-8")
		body := "name: test\ndescription: test\nruns: {using: composite, steps: []}\n"
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	}
	return nil, fmt.Errorf("unexpected request authority %s", request.URL.Host)
}

func TestBundledCommitishListsDifferentRepositoriesConcurrently(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct{ host, ref string }{{"github.com", "v1"}, {"raw.githubusercontent.com", commit}} {
		t.Run(tc.host, func(t *testing.T) {
			transport := &barrierTransport{host: tc.host, released: make(chan struct{}), commit: commit}
			provider, err := github.New(github.Options{Mode: github.Online, Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			workflow := fmt.Sprintf("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: example/first@%s\n      - uses: example/second@%s\n", tc.ref, tc.ref)
			result, err := CheckWithPlugins(t.Context(), Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, RemoteActions: provider}, inputmodel.Request{Kind: inputmodel.Stdin, Name: "workflow.yml", Reader: strings.NewReader(workflow)}, plugininstall.Sources{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ActionMetadata) != 2 {
				t.Fatalf("coverage=%+v", result.ActionMetadata)
			}
			for _, coverage := range result.ActionMetadata {
				if coverage.Availability.State() != actions.MetadataAvailable {
					t.Fatalf("coverage=%+v", result.ActionMetadata)
				}
			}
		})
	}
}
