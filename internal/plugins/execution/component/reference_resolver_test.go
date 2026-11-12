package component

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
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

func knownRepositoryID(tb testing.TB, value uint64) repositoryid.ID {
	tb.Helper()
	id, err := repositoryid.Known(value)
	if err != nil {
		tb.Fatal(err)
	}
	return id
}

func newResolverRepositoryTest(tb testing.TB) (*repositoryObjects, *resolverRepositoryAccess, *resolverRepositorySession) {
	tb.Helper()
	session := &resolverRepositorySession{
		identity: repository.Identity{Host: "github.com", ID: knownRepositoryID(tb, 7), Name: "owner/repo", ObservedAt: "2026-11-10T00:00:00Z"},
		refs:     map[string]string{"refs/tags/v1": strings.Repeat("a", 40)},
		objects:  map[string]repository.Object{strings.Repeat("a", 40): repository.Commit{}},
	}
	access := &resolverRepositoryAccess{session: session}
	objects := &repositoryObjects{request: references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: knownRepositoryID(tb, 7)}, access: access, active: true}
	return objects, access, session
}

func TestReferenceResolverRepositoryScope(t *testing.T) {
	t.Parallel()
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
		{"unexpected identity", func(objects *repositoryObjects) error {
			objects.request.ExpectedRepositoryID = repositoryid.ID{}
			_, err := objects.open(t.Context(), "owner/repo", repositorybind.SomeU64(7))
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
			t.Parallel()
			objects, access, session := newResolverRepositoryTest(t)
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
			t.Parallel()
			objects, access, session := newResolverRepositoryTest(t)
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
	t.Parallel()
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
			t.Parallel()
			objects, _, session := newResolverRepositoryTest(t)
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
			result := references.Result{Version: 1, RepositoryID: knownRepositoryID(t, 7), Repository: session.identity.Name, ObservedAt: session.identity.ObservedAt, SelectedObject: selected, SelectedRef: "refs/tags/v1", Commit: tc.commit, Tree: tc.tree}
			if err := objects.validate(result); err != nil {
				t.Fatal(err)
			}
			for _, change := range []func(*references.Result){
				func(result *references.Result) { result.RepositoryID = knownRepositoryID(t, 8) },
				func(result *references.Result) { result.RepositoryID = repositoryid.ID{} },
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
	t.Parallel()
	objects, _, session := newResolverRepositoryTest(t)
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
	t.Parallel()
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
	t.Parallel()
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
	request.ExpectedRepositoryID = knownRepositoryID(t, 7)
	if _, err := session.Resolve(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("requests with different consumer or identity shared a cache entry: %d", calls.Load())
	}
}

func TestReferenceResolverSessionCoalescesAndCancelsWaiter(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	session := &referenceResolverSession{resolver: referenceResolverFunc(func(context.Context, references.Request) (references.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return references.Result{RepositoryID: knownRepositoryID(t, 7)}, nil
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
	if err != nil || result.RepositoryID != knownRepositoryID(t, 7) || calls.Load() != 1 {
		t.Fatalf("result=%+v calls=%d error=%v", result, calls.Load(), err)
	}
}

func TestReferenceResolverSessionWaiterRetriesAbandonedResolution(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		session := &referenceResolverSession{resolver: referenceResolverFunc(func(ctx context.Context, _ references.Request) (references.Result, error) {
			if calls.Add(1) == 1 {
				<-ctx.Done()
				return references.Result{}, ctx.Err()
			}
			return references.Result{RepositoryID: knownRepositoryID(t, 7)}, nil
		})}
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action"}
		initiator, cancel := context.WithCancel(t.Context())
		first := make(chan error, 1)
		go func() { _, err := session.Resolve(initiator, request); first <- err }()
		synctest.Wait()
		waiter, stop := references.StartLookupBudget(t.Context(), time.Second)
		defer stop()
		second := make(chan error, 1)
		var result references.Result
		go func() {
			var err error
			result, err = session.Resolve(waiter, request)
			second <- err
		}()
		synctest.Wait()
		time.Sleep(time.Minute)
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatalf("initiator error = %v", err)
		}
		if err := <-second; err != nil || result.RepositoryID != knownRepositoryID(t, 7) || calls.Load() != 2 || waiter.Err() != nil {
			t.Fatalf("waiter result=%+v error=%v calls=%d budget=%v", result, err, calls.Load(), context.Cause(waiter))
		}
	})
}

func TestReferenceResolverGateWaitIsOutsideLookupBudget(t *testing.T) {
	t.Parallel()
	plugin, err := LoadArtifact(t.Context(), commitish.Artifact(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := plugin.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		if err := plugin.gate.Acquire(t.Context(), 1); err != nil {
			t.Fatal(err)
		}
		_, access, _ := newResolverRepositoryTest(t)
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: knownRepositoryID(t, 7)}
		budgeted, stop := references.StartLookupBudget(repository.WithAccess(t.Context(), access), time.Second)
		defer stop()
		resolved := make(chan error, 1)
		go func() {
			_, err := plugin.Resolve(budgeted, request)
			resolved <- err
		}()
		synctest.Wait()
		time.Sleep(time.Minute)
		plugin.gate.Release(1)
		if err := <-resolved; err != nil || budgeted.Err() != nil {
			t.Fatalf("resolution=%v budget=%v", err, context.Cause(budgeted))
		}
	})
}

func TestBundledReferenceResolverCallsScopedRepositoryHost(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			_, access, session := newResolverRepositoryTest(t)
			session.refs, session.objects = tc.refs, tc.objects
			request := references.Request{Version: 1, Repository: "owner/repo", Ref: tc.reference, Kind: tc.kind, ExpectedRepositoryID: knownRepositoryID(t, 7)}
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
		t.Parallel()
		repositoryID := knownRepositoryID(t, 9007199254740999)
		_, access, session := newResolverRepositoryTest(t)
		session.identity.ID = repositoryID
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: repositoryID}
		result, err := plugin.Resolve(repository.WithAccess(t.Context(), access), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(access.requests) != 1 || access.requests[0].ExpectedRepositoryID != repositoryID {
			t.Fatalf("host received requests %+v; expected repository ID %+v", access.requests, repositoryID)
		}
		if session.Repository().ID != repositoryID || result.RepositoryID != repositoryID {
			t.Fatalf("host repository ID = %+v, result repository ID = %+v; want %+v", session.Repository().ID, result.RepositoryID, repositoryID)
		}
		if err := references.ValidateResult(request, result); err != nil {
			t.Fatal(err)
		}
	})
	for _, tc := range []struct {
		name     string
		expected repositoryid.ID
	}{
		{"absent host and expected repository IDs", repositoryid.ID{}},
		{"absent host repository ID with an expected ID", knownRepositoryID(t, 7)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, access, session := newResolverRepositoryTest(t)
			session.identity.ID = repositoryid.ID{}
			request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: tc.expected}
			result, err := plugin.Resolve(repository.WithAccess(t.Context(), access), request)
			if err != nil {
				t.Fatal(err)
			}
			if !result.RepositoryID.IsZero() || result.Repository != "owner/repo" || result.Commit != selected || result.SelectedRef != "refs/tags/v1" {
				t.Fatalf("result=%+v", result)
			}
			if err := references.ValidateResult(request, result); err != nil {
				t.Fatal(err)
			}
			if len(access.requests) != 1 || access.requests[0] != request || session.finishes != 1 {
				t.Fatalf("requests=%+v finishes=%d", access.requests, session.finishes)
			}
		})
	}
	t.Run("conflicting host repository ID", func(t *testing.T) {
		t.Parallel()
		_, access, session := newResolverRepositoryTest(t)
		session.identity.ID = knownRepositoryID(t, 8)
		request := references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: knownRepositoryID(t, 7)}
		if result, err := plugin.Resolve(repository.WithAccess(t.Context(), access), request); err == nil {
			t.Fatalf("accepted a conflicting repository ID: %+v", result)
		}
		if session.lookups != 0 || session.finishes != 0 {
			t.Fatalf("lookups=%d finishes=%d after identity conflict", session.lookups, session.finishes)
		}
	})
}

func TestLiftReferenceResolutionRepositoryID(t *testing.T) {
	t.Parallel()
	resolution := func(id wacogo.Val) wacogo.Val {
		return wacogo.ValResultOk(wacogo.NewValRecord(
			field("repository-id", id),
			field("repository", wacogo.ValString("owner/repo")),
			field("observed-at", wacogo.ValString("2026-11-12T00:00:00Z")),
			field("selected-object", wacogo.ValString(strings.Repeat("a", 40))),
			field("selection", wacogo.NewValVariant(0, nil)),
			field("content", wacogo.NewValVariant(0, wacogo.ValString(strings.Repeat("a", 40)))),
		))
	}
	result, err := liftReferenceResolution(resolution(wacogo.ValOptionNone()))
	if err != nil || !result.RepositoryID.IsZero() {
		t.Fatalf("absent ID lifted as %+v, %v", result.RepositoryID, err)
	}
	result, err = liftReferenceResolution(resolution(wacogo.ValOptionSome(wacogo.ValU64(42))))
	if err != nil || result.RepositoryID != knownRepositoryID(t, 42) {
		t.Fatalf("known ID lifted as %+v, %v", result.RepositoryID, err)
	}
	for name, id := range map[string]wacogo.Val{
		"zero":       wacogo.ValOptionSome(wacogo.ValU64(0)),
		"wrong type": wacogo.ValOptionSome(wacogo.ValString("42")),
		"not option": wacogo.ValU64(42),
	} {
		if _, err := liftReferenceResolution(resolution(id)); err == nil {
			t.Errorf("%s repository ID was accepted", name)
		}
	}
}

func TestLowerReferenceRequestRepositoryID(t *testing.T) {
	t.Parallel()
	for _, expected := range []repositoryid.ID{{}, knownRepositoryID(t, 42)} {
		value, err := lowerReferenceRequest(references.Request{Version: 1, Repository: "owner/repo", Ref: "v1", Kind: "action", ExpectedRepositoryID: expected})
		if err != nil {
			t.Fatal(err)
		}
		record, ok := value.(*wacogo.ValRecord)
		if !ok {
			t.Fatalf("request lowered as %T", value)
		}
		option, ok := record.Field("expected-repository-id").(*wacogo.ValOption)
		if !ok {
			t.Fatalf("expected-repository-id lowered as %T", record.Field("expected-repository-id"))
		}
		want, known := expected.Value()
		if option.IsNone() == known || known && option.Val() != wacogo.ValU64(want) {
			t.Fatalf("expected-repository-id = %+v for %+v", option, expected)
		}
	}
}
