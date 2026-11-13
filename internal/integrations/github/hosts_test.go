package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"actionlint.kjanat.dev/v2/internal/development/tests/support/gitwire"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
)

const (
	rawText      = "text/plain; charset=utf-8"
	jsDelivrYAML = "text/yaml; charset=utf-8"
)

func respond(r *http.Request, status int, contentType, body string) *http.Response {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func listing(head string, refs ...gitwire.Ref) string {
	return string(gitwire.RefAdvertisement(head, "refs/heads/main", refs...))
}

type recorder struct {
	mu       sync.Mutex
	requests []string
	handle   func(*http.Request) (*http.Response, error)
}

func (r *recorder) RoundTrip(request *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request.URL.Host+request.URL.RequestURI())
	r.mu.Unlock()
	return r.handle(request)
}

func (r *recorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

func (r *recorder) count(host string) int {
	n := 0
	for _, request := range r.recorded() {
		if strings.HasPrefix(request, host+"/") {
			n++
		}
	}
	return n
}

type listingResolver struct {
	mu    sync.Mutex
	calls int
}

func (r *listingResolver) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	access := repository.AccessFromContext(ctx)
	if access == nil {
		return references.Result{}, errors.New("no repository access")
	}
	session, err := access.Open(ctx, request)
	if err != nil {
		return references.Result{}, err
	}
	candidates := []string{"refs/tags/" + request.Ref, "refs/heads/" + request.Ref}
	if request.Ref == "HEAD" || strings.HasPrefix(request.Ref, "refs/") {
		candidates = []string{request.Ref}
	}
	selected, object := "", ""
	if fullSHA1(request.Ref) {
		candidates, object = nil, strings.ToLower(request.Ref)
	}
	for _, name := range candidates {
		id, found, err := session.Reference(ctx, name)
		if err != nil {
			return references.Result{}, err
		}
		if found {
			selected, object = name, id
			break
		}
	}
	if object == "" {
		return references.Result{}, errors.New("reference not found")
	}
	kind, found, err := session.Object(ctx, object)
	if err != nil || !found {
		return references.Result{}, errors.Join(errors.New("selected object not found"), err)
	}
	commit := object
	if tag, ok := kind.(repository.PeeledTag); ok {
		commit = tag.Commit
		if _, found, err := session.Object(ctx, commit); err != nil || !found {
			return references.Result{}, errors.Join(errors.New("peeled commit not found"), err)
		}
	}
	log, err := session.Finish(ctx)
	if err != nil {
		return references.Result{}, err
	}
	identity := session.Repository()
	return references.Result{Version: 1, Repository: identity.Name, RepositoryID: identity.ID, Commit: commit, SelectedObject: object, SelectedRef: selected, ObservedAt: identity.ObservedAt, FetchLog: log}, nil
}

func (r *listingResolver) resolutions() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func newListingClient(t *testing.T, options Options, resolver references.Resolver) *GitHub {
	t.Helper()
	var client *GitHub
	options.Resolver = func() references.Resolver { return repository.Bind(resolver, client) }
	client, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
