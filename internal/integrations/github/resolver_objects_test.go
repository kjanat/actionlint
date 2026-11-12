package github

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
)

func TestResolverRepositoryRechecksReferences(t *testing.T) {
	t.Parallel()
	const id = "0123456789012345678901234567890123456789"
	for _, changed := range []bool{false, true} {
		calls := 0
		g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
			status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
			switch r.URL.Path {
			case "/repos/example/action":
			case "/repos/example/action/contents":
				body = `[]`
			case "/repos/example/action/git/ref/tags/v1":
				calls++
				if changed && calls > 1 {
					status = 404
					body = ""
				} else {
					body = fmt.Sprintf(`{"ref":"refs/tags/v1","object":{"sha":%q,"type":"commit"}}`, id)
				}
			case "/repos/example/action/git/commits/" + id:
				body = fmt.Sprintf(`{"sha":%q}`, id)
			default:
				t.Fatalf("unexpected request %s", r.URL)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})})
		if err != nil {
			t.Fatal(err)
		}
		session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action", ExpectedRepositoryID: knownRepositoryID(t, 42)})
		if err != nil {
			t.Fatal(err)
		}
		if session.Repository().ID != knownRepositoryID(t, 42) {
			t.Fatalf("session repository ID = %+v", session.Repository().ID)
		}
		got, found, err := session.Reference(t.Context(), "refs/tags/v1")
		if err != nil || !found || got != id {
			t.Fatalf("reference = %q %v %v", got, found, err)
		}
		object, found, err := session.Object(t.Context(), id)
		if err != nil || !found {
			t.Fatalf("object = %v %v %v", object, found, err)
		}
		if _, ok := object.(repository.Commit); !ok {
			t.Fatalf("object = %T", object)
		}
		log, err := session.Finish(t.Context())
		if (err != nil) != changed {
			t.Fatalf("finish = %s, %v", log, err)
		}
	}
}

func TestResolverRepositoryOfflineDoesNotRequest(t *testing.T) {
	t.Parallel()
	for _, mode := range []Mode{Off, Offline} {
		g, err := New(Options{Mode: mode, Transport: transport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected request"); return nil, nil })})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"}); err == nil {
			t.Fatal("expected unavailable error")
		}
	}
}

func TestResolverMissingRefRequiresReadableContents(t *testing.T) {
	t.Parallel()
	contents := 0
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		if strings.HasSuffix(r.URL.Path, "/contents") {
			contents++
			body = `[]`
			if contents > 1 {
				status = 404
			}
		} else if strings.Contains(r.URL.Path, "/git/ref/") {
			status = 404
			body = ""
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Reference(t.Context(), "refs/tags/missing"); err == nil {
		t.Fatal("accepted inaccessible repository as missing reference")
	}
}

func TestResolverAnnotatedTagPeelsToCommit(t *testing.T) {
	t.Parallel()
	const tagID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const commitID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
		case "/repos/example/action/contents":
			body = `[]`
		case "/repos/example/action/git/commits/" + tagID:
			status, body = 404, ""
		case "/repos/example/action/git/trees/" + tagID:
			status, body = 422, ""
		case "/repos/example/action/git/tags/" + tagID:
			body = fmt.Sprintf(`{"sha":%q,"object":{"sha":%q,"type":"commit"}}`, tagID, commitID)
		case "/repos/example/action/git/commits/" + commitID:
			body = fmt.Sprintf(`{"sha":%q}`, commitID)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	object, found, err := session.Object(t.Context(), tagID)
	if err != nil || !found {
		t.Fatalf("object = %v %v %v", object, found, err)
	}
	tag, ok := object.(repository.PeeledTag)
	if !ok || tag.Commit != commitID {
		t.Fatalf("object = %#v", object)
	}
	if _, _, err := session.Object(t.Context(), strings.Repeat("a", 64)); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("SHA-256 error = %v", err)
	}
}

func TestResolverRejectsInvalidTagChains(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		target   func(string) (string, string)
		expected string
	}{
		{"cycle", func(id string) (string, string) { return id, "tag" }, "cycle"},
		{"type mismatch", func(string) (string, string) { return strings.Repeat("b", 40), "commit" }, "type does not match"},
		{"depth limit", func(id string) (string, string) {
			var n uint64
			_, err := fmt.Sscanf(id, "%x", &n)
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%040x", n+1), "tag"
		}, "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
				switch {
				case r.URL.Path == "/repos/example/action":
				case r.URL.Path == "/repos/example/action/contents":
					body = `[]`
				case strings.Contains(r.URL.Path, "/git/commits/"):
					status, body = 404, ""
				case strings.Contains(r.URL.Path, "/git/trees/"):
					id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
					if tc.name == "type mismatch" && id == strings.Repeat("b", 40) {
						body = fmt.Sprintf(`{"sha":%q}`, id)
					} else {
						status, body = 404, ""
					}
				case strings.Contains(r.URL.Path, "/git/tags/"):
					id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
					if tc.name == "type mismatch" && id == strings.Repeat("b", 40) {
						status, body = 404, ""
					} else {
						target, kind := tc.target(id)
						body = fmt.Sprintf(`{"sha":%q,"object":{"sha":%q,"type":%q}}`, id, target, kind)
					}
				default:
					t.Fatalf("unexpected request %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = session.Object(t.Context(), fmt.Sprintf("%040x", 1))
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("object error = %v, want %s", err, tc.expected)
			}
		})
	}
}

func TestResolverRechecksAbsentReference(t *testing.T) {
	t.Parallel()
	calls := 0
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
		case "/repos/example/action/contents":
			body = `[]`
		case "/repos/example/action/git/ref/pull/1/head":
			calls++
			if calls == 1 {
				status, body = 404, ""
			} else {
				body = `{"ref":"refs/pull/1/head","object":{"sha":"0123456789012345678901234567890123456789","type":"commit"}}`
			}
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := session.Reference(t.Context(), "refs/pull/1/head")
	if err != nil || found {
		t.Fatalf("reference = %v %v", found, err)
	}
	if _, err := session.Finish(t.Context()); err == nil {
		t.Fatal("accepted a reference created during resolution")
	}
}

func TestResolverFinishRejectsReplacedRepositoryWithSameRef(t *testing.T) {
	t.Parallel()
	const id = "0123456789012345678901234567890123456789"
	reads := 0
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
			if reads >= 2 {
				body = `{"id":43,"full_name":"example/action","default_branch":"main"}`
			}
		case "/repos/example/action/contents":
			body = `[]`
		case "/repos/example/action/git/ref/tags/v1":
			reads++
			body = fmt.Sprintf(`{"ref":"refs/tags/v1","object":{"sha":%q,"type":"commit"}}`, id)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := session.Reference(t.Context(), "refs/tags/v1"); err != nil || !found {
		t.Fatalf("reference = %v %v", found, err)
	}
	if _, err := session.Finish(t.Context()); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("finish error = %v", err)
	}
}

func TestResolverAcceptsSixteenAnnotatedTags(t *testing.T) {
	t.Parallel()
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := 200, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		var n uint64
		if len(id) == 40 {
			if _, err := fmt.Sscanf(id, "%x", &n); err != nil {
				t.Fatal(err)
			}
		}
		switch {
		case r.URL.Path == "/repos/example/action":
		case r.URL.Path == "/repos/example/action/contents":
			body = `[]`
		case strings.Contains(r.URL.Path, "/git/commits/"):
			if n == 17 {
				body = fmt.Sprintf(`{"sha":%q}`, id)
			} else {
				status, body = 404, ""
			}
		case strings.Contains(r.URL.Path, "/git/trees/"):
			status, body = 404, ""
		case strings.Contains(r.URL.Path, "/git/tags/"):
			target := fmt.Sprintf("%040x", n+1)
			kind := "tag"
			if n == 16 {
				kind = "commit"
			}
			body = fmt.Sprintf(`{"sha":%q,"object":{"sha":%q,"type":%q}}`, id, target, kind)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	object, found, err := session.Object(t.Context(), fmt.Sprintf("%040x", 1))
	if err != nil || !found {
		t.Fatalf("object = %v %v %v", object, found, err)
	}
	tag, ok := object.(repository.PeeledTag)
	if !ok || tag.Commit != fmt.Sprintf("%040x", 17) {
		t.Fatalf("object = %#v", object)
	}
}

func TestResolverDoesNotTreatTree422AsMissing(t *testing.T) {
	t.Parallel()
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
		case "/repos/example/action/contents":
			body = `[]`
		case "/repos/example/action/git/commits/" + id, "/repos/example/action/git/tags/" + id:
			status, body = http.StatusNotFound, ""
		case "/repos/example/action/git/trees/" + id:
			status, body = http.StatusUnprocessableEntity, ""
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	if object, found, err := session.Object(t.Context(), id); err == nil || found {
		t.Fatalf("object = %v, found = %v, error = %v; want an error", object, found, err)
	}
}

func TestResolverReadsTreeAfterCommitAndTagMisses(t *testing.T) {
	t.Parallel()
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		status, body := http.StatusOK, `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
		case "/repos/example/action/contents":
			body = `[]`
		case "/repos/example/action/git/commits/" + id, "/repos/example/action/git/tags/" + id:
			status, body = http.StatusNotFound, ""
		case "/repos/example/action/git/trees/" + id:
			body = fmt.Sprintf(`{"sha":%q}`, id)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil {
		t.Fatal(err)
	}
	object, found, err := session.Object(t.Context(), id)
	if err != nil || !found {
		t.Fatalf("object = %v, found = %v, error = %v", object, found, err)
	}
	if _, ok := object.(repository.Tree); !ok {
		t.Fatalf("object = %T, want repository.Tree", object)
	}
}

func TestResolverRepositoryComparesKnownExpectedID(t *testing.T) {
	t.Parallel()
	g, err := New(Options{Transport: transport(func(r *http.Request) (*http.Response, error) {
		body := `{"id":42,"full_name":"example/action","default_branch":"main"}`
		switch r.URL.Path {
		case "/repos/example/action":
		case "/repos/example/action/contents":
			body = `[]`
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	session, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action"})
	if err != nil || session.Repository().ID != knownRepositoryID(t, 42) {
		t.Fatalf("open without expected ID = %v, %v", session, err)
	}
	if _, err := g.Open(t.Context(), references.Request{Version: 1, Repository: "example/action", ExpectedRepositoryID: knownRepositoryID(t, 99)}); err == nil {
		t.Fatal("opened a repository with a conflicting ID")
	}
}
