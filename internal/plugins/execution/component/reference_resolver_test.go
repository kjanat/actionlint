package component

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/plugins/bundled/commitish"
	repositorybind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-resolver/repositoryobjects"
	"github.com/kjanat/wacogo"
)

type resolverRepositoryAccess struct {
	session  repository.Session
	requests []references.Request
}

func (a *resolverRepositoryAccess) Open(_ context.Context, request references.Request) (repository.Session, error) {
	a.requests = append(a.requests, request)
	return a.session, nil
}

type resolverRepositorySession struct {
	identity  repository.Identity
	refs      map[string]string
	objects   map[string]repository.Object
	lookups   int
	finishes  int
	finishErr error
}

func (s *resolverRepositorySession) Repository() repository.Identity { return s.identity }
func (s *resolverRepositorySession) Reference(_ context.Context, name string) (string, bool, error) {
	s.lookups++
	id, found := s.refs[name]
	return id, found, nil
}

func (s *resolverRepositorySession) Object(_ context.Context, id string) (repository.Object, bool, error) {
	s.lookups++
	object, found := s.objects[id]
	return object, found, nil
}

func (s *resolverRepositorySession) Finish(context.Context) (json.RawMessage, error) {
	s.finishes++
	return json.RawMessage(`{"source":"host"}`), s.finishErr
}

func newResolverRepositoryTest() (*repositoryObjects, *resolverRepositoryAccess, *resolverRepositorySession) {
	session := &resolverRepositorySession{
		identity: repository.Identity{Host: "github.com", ID: 7, Name: "owner/repo", ObservedAt: "2026-11-10T00:00:00Z"},
		refs:     map[string]string{"refs/tags/v1": strings.Repeat("a", 40)},
		objects:  map[string]repository.Object{strings.Repeat("a", 40): repository.Commit{}},
	}
	access := &resolverRepositoryAccess{session: session}
	objects := &repositoryObjects{request: references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: 7}, access: access, active: true}
	return objects, access, session
}

func TestReferenceResolverRepositoryScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*repositoryObjects) error
	}{
		{"outside resolve", func(objects *repositoryObjects) error {
			objects.active = false
			_, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(7))
			return err
		}},
		{"different repository", func(objects *repositoryObjects) error {
			_, err := objects.open(t.Context(), "other/repo", repositorybind.SomeU64(7))
			return err
		}},
		{"different identity", func(objects *repositoryObjects) error {
			_, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(8))
			return err
		}},
		{"missing identity", func(objects *repositoryObjects) error {
			_, err := objects.open(t.Context(), "owner/repo", repositorybind.NoneU64())
			return err
		}},
		{"reference before open", func(objects *repositoryObjects) error {
			value, err := objects.GetRef(t.Context(), "refs/tags/v1")
			if err != nil {
				return err
			}
			if failure, ok := value.(repositorybind.ResultOptionStringStringErr); ok {
				return errors.New(failure.Value)
			}
			return nil
		}},
		{"object before open", func(objects *repositoryObjects) error {
			value, err := objects.GetObject(t.Context(), strings.Repeat("a", 40))
			if err != nil {
				return err
			}
			if failure, ok := value.(repositorybind.ResultOptionObjectStringErr); ok {
				return errors.New(failure.Value)
			}
			return nil
		}},
		{"finish before open", func(objects *repositoryObjects) error { return objects.finish(t.Context()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects, access, session := newResolverRepositoryTest()
			if err := tc.call(objects); err == nil {
				t.Fatal("misuse succeeded")
			}
			if len(access.requests) != 0 || session.lookups != 0 || session.finishes != 0 {
				t.Fatal("misuse reached repository access")
			}
			if objects.err == nil {
				t.Fatal("misuse could be ignored before publishing a result")
			}
		})
	}
	for _, operation := range []string{"second open", "reference after finish", "object after finish", "second finish"} {
		t.Run(operation, func(t *testing.T) {
			objects, access, session := newResolverRepositoryTest()
			if _, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(7)); err != nil {
				t.Fatal(err)
			}
			if err := objects.finish(t.Context()); err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "second open":
				_, _ = objects.Open(t.Context(), "owner/repo", repositorybind.SomeU64(7))
			case "reference after finish":
				_, _ = objects.GetRef(t.Context(), "refs/tags/v1")
			case "object after finish":
				_, _ = objects.GetObject(t.Context(), strings.Repeat("a", 40))
			case "second finish":
				_ = objects.finish(t.Context())
			}
			if len(access.requests) != 1 || session.lookups != 0 || session.finishes != 1 || objects.err == nil {
				t.Fatalf("access=%+v session=%+v err=%v", access.requests, session, objects.err)
			}
		})
	}
}

func TestReferenceResolverValidatesObservedContent(t *testing.T) {
	selected := strings.Repeat("a", 40)
	peeled := strings.Repeat("b", 40)
	for _, tc := range []struct {
		name   string
		object repository.Object
		commit string
		tree   string
	}{
		{"commit", repository.Commit{}, selected, ""},
		{"tree", repository.Tree{}, "", selected},
		{"peeled commit", repository.PeeledTag{Commit: peeled}, peeled, ""},
		{"peeled tree", repository.PeeledTreeTag{Tree: peeled}, "", peeled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects, _, session := newResolverRepositoryTest()
			session.objects[selected] = tc.object
			if tc.commit == peeled {
				session.objects[peeled] = repository.Commit{}
			}
			if tc.tree == peeled {
				session.objects[peeled] = repository.Tree{}
			}
			if _, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(7)); err != nil {
				t.Fatal(err)
			}
			if _, err := objects.GetRef(t.Context(), "refs/tags/v1"); err != nil {
				t.Fatal(err)
			}
			if _, err := objects.GetObject(t.Context(), selected); err != nil {
				t.Fatal(err)
			}
			if tc.commit == peeled || tc.tree == peeled {
				if _, err := objects.GetObject(t.Context(), peeled); err != nil {
					t.Fatal(err)
				}
			}
			if err := objects.finish(t.Context()); err != nil {
				t.Fatal(err)
			}
			result := references.Result{Version: 1, RepositoryID: 7, Repository: session.identity.Name, ObservedAt: session.identity.ObservedAt, SelectedObject: selected, SelectedRef: "refs/tags/v1", Commit: tc.commit, Tree: tc.tree}
			if err := objects.validate(result); err != nil {
				t.Fatal(err)
			}
			for _, change := range []func(*references.Result){
				func(result *references.Result) { result.RepositoryID++ },
				func(result *references.Result) { result.Repository = "other/repo" },
				func(result *references.Result) { result.ObservedAt = "2026-11-10T00:00:01Z" },
				func(result *references.Result) { result.SelectedRef = "refs/heads/missing" },
				func(result *references.Result) { result.SelectedObject = peeled },
				func(result *references.Result) { result.Commit = strings.Repeat("c", 40) },
				func(result *references.Result) { result.Tree = strings.Repeat("c", 40) },
			} {
				forged := result
				change(&forged)
				if err := objects.validate(forged); err == nil {
					t.Fatalf("accepted forged result %+v", forged)
				}
			}
			objects.finished = false
			if err := objects.validate(result); err == nil {
				t.Fatal("accepted unfinished lookup")
			}
		})
	}
}

func TestReferenceResolverRejectsFailedFinish(t *testing.T) {
	objects, _, session := newResolverRepositoryTest()
	session.finishErr = errors.New("repository identity changed")
	if _, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(7)); err != nil {
		t.Fatal(err)
	}
	if err := objects.finish(t.Context()); !errors.Is(err, session.finishErr) {
		t.Fatalf("finish error = %v", err)
	}
	if err := objects.validate(references.Result{}); !errors.Is(err, session.finishErr) {
		t.Fatalf("validation error = %v", err)
	}
}

func TestReferenceResolverRoleAndImports(t *testing.T) {
	roles, err := extensionRoles([]wacogo.ExportDesc{{Name: ReferenceResolverInterface, Kind: wacogo.SortInstance}})
	if err != nil || !roles.referenceResolver {
		t.Fatalf("roles=%+v error=%v", roles, err)
	}
	if _, err := extensionRoles([]wacogo.ExportDesc{{Name: "kjanat:actionlint-resolver/reference-resolver@2.0.0", Kind: wacogo.SortInstance}}); err == nil {
		t.Fatal("accepted unsupported resolver version")
	}
	if _, err := extensionRoles([]wacogo.ExportDesc{{Name: ReferenceResolverInterface, Kind: wacogo.SortFunc}}); err == nil {
		t.Fatal("accepted function as resolver interface")
	}
	required, err := validateImports([]wacogo.ImportDesc{{Name: RepositoryObjectsInterface, Kind: wacogo.SortInstance}})
	if err != nil || !required.repositoryObjects {
		t.Fatalf("required=%+v error=%v", required, err)
	}
	if _, err := validateImports([]wacogo.ImportDesc{{Name: "wasi:sockets/tcp@0.2.0", Kind: wacogo.SortInstance}}); err == nil {
		t.Fatal("granted socket access")
	}
}

type referenceResolverFunc func(context.Context, references.Request) (references.Result, error)

func (f referenceResolverFunc) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	return f(ctx, request)
}

func TestReferenceResolverSessionCachesExactRequests(t *testing.T) {
	var calls atomic.Int64
	session := &referenceResolverSession{resolver: referenceResolverFunc(func(context.Context, references.Request) (references.Result, error) {
		calls.Add(1)
		return references.Result{FetchLog: json.RawMessage(`{"source":"host"}`)}, nil
	})}
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
	result, err := session.Resolve(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	result.FetchLog[0] = 'x'
	cached, err := session.Resolve(t.Context(), request)
	if err != nil || !json.Valid(cached.FetchLog) || calls.Load() != 1 {
		t.Fatalf("cached=%s calls=%d err=%v", cached.FetchLog, calls.Load(), err)
	}
	request.Kind = "workflow"
	if _, err := session.Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	request.ExpectedRepositoryID = 7
	if _, err := session.Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("requests with different consumer or identity shared a cache entry: %d", calls.Load())
	}
}

func TestReferenceResolverSessionCoalescesAndCancelsWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	session := &referenceResolverSession{resolver: referenceResolverFunc(func(context.Context, references.Request) (references.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return references.Result{RepositoryID: 7}, nil
	})}
	request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
	first := make(chan error, 1)
	go func() { _, err := session.Resolve(t.Context(), request); first <- err }()
	<-started
	waiter, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := session.Resolve(waiter, request)
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v", err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	result, err := session.Resolve(t.Context(), request)
	if err != nil || result.RepositoryID != 7 || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d error=%v", result, calls.Load(), err)
	}
}

func TestBundledReferenceResolverCallsScopedRepositoryHost(t *testing.T) {
	plugin, err := LoadArtifact(t.Context(), commitish.Artifact(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := plugin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if !plugin.HasReferenceResolver() || plugin.HasRunnerProvider() || plugin.HasScriptEngine() {
		t.Fatal("incorrect bundled resolver roles")
	}
	selected, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name        string
		reference   string
		kind        string
		refs        map[string]string
		objects     map[string]repository.Object
		selectedRef string
		commit      string
		tree        string
	}{
		{"action tag before branch", "v1", "action", map[string]string{"refs/tags/v1": selected, "refs/heads/v1": other}, map[string]repository.Object{selected: repository.Commit{}, other: repository.Commit{}}, "refs/tags/v1", selected, ""},
		{"uppercase action object ID", strings.ToUpper(selected), "action", nil, map[string]repository.Object{selected: repository.Commit{}}, "", selected, ""},
		{"workflow tree tag", "v1", "workflow", map[string]string{"refs/tags/v1": selected}, map[string]repository.Object{selected: repository.PeeledTreeTag{Tree: other}, other: repository.Tree{}}, "refs/tags/v1", "", other},
		{"action annotated commit tag", "v1", "action", map[string]string{"refs/tags/v1": selected}, map[string]repository.Object{selected: repository.PeeledTag{Commit: other}, other: repository.Commit{}}, "refs/tags/v1", other, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, access, session := newResolverRepositoryTest()
			session.refs, session.objects = tc.refs, tc.objects
			request := references.Request{Version: 1, Repository: "owner/repo", Ref: tc.reference, Kind: tc.kind, ExpectedRepositoryID: 7}
			result, err := plugin.Resolve(repository.WithAccess(t.Context(), access), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := references.ValidateResult(request, result); err != nil {
				t.Fatal(err)
			}
			if result.SelectedObject != selected || result.SelectedRef != tc.selectedRef || result.Commit != tc.commit || result.Tree != tc.tree || result.Network != nil || string(result.FetchLog) != `{"source":"host"}` {
				t.Fatalf("result=%+v", result)
			}
			if len(access.requests) != 1 || access.requests[0] != request || session.finishes != 1 {
				t.Fatalf("requests=%+v finishes=%d", access.requests, session.finishes)
			}
		})
	}
	t.Run("repository ID above float64 integer precision", func(t *testing.T) {
		const repositoryID uint64 = 9007199254740999
		_, access, session := newResolverRepositoryTest()
		session.identity.ID = repositoryID
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: repositoryID}
		result, err := plugin.Resolve(repository.WithAccess(t.Context(), access), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(access.requests) != 1 || access.requests[0].ExpectedRepositoryID != repositoryID {
			t.Fatalf("host received requests %+v; expected repository ID %d", access.requests, repositoryID)
		}
		if session.Repository().ID != repositoryID || result.RepositoryID != repositoryID {
			t.Fatalf("host repository ID = %d, result repository ID = %d; want %d", session.Repository().ID, result.RepositoryID, repositoryID)
		}
		if err := references.ValidateResult(request, result); err != nil {
			t.Fatal(err)
		}
	})
}
