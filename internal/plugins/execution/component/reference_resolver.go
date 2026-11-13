package component

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/references/repository"
	"actionlint.kjanat.dev/v2/internal/integrations/http/accounting"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	repositorybind "actionlint.kjanat.dev/v2/internal/plugins/protocol/component/kjanat/actionlint-resolver/repositoryobjects"
	"github.com/kjanat/wacogo"
)

const RepositoryObjectsInterface = repositorybind.InterfaceName

var (
	_ references.Resolver       = (*Plugin)(nil)
	_ references.SessionFactory = (*Plugin)(nil)
)

func (p *Plugin) HasReferenceResolver() bool { return p.referenceResolver }

func (p *Plugin) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	defer accounting.RecordResolverCall(ctx, &references.RequestReport{AllRequestsReported: true}, false)
	if !p.HasReferenceResolver() {
		return references.Result{}, errors.New("component does not implement the reference-resolver role")
	}
	input, err := lowerReferenceRequest(request)
	if err != nil {
		return references.Result{}, err
	}
	objects := &repositoryObjects{request: request, access: repository.AccessFromContext(ctx)}
	ctx = context.WithValue(ctx, repositoryCallKey{}, objects)
	value, err := p.callInterface(ctx, ReferenceResolverInterface, "resolve", input)
	if err != nil {
		return references.Result{}, err
	}
	result, err := liftReferenceResolution(value)
	if err != nil {
		return references.Result{}, err
	}
	if err := objects.validate(result); err != nil {
		return references.Result{}, err
	}
	result.FetchLog = append(json.RawMessage(nil), objects.fetchLog...)
	if err := references.ValidateResult(request, result); err != nil {
		return references.Result{}, err
	}
	return result, nil
}

// NewSession shares exact lookups within one analysis.
func (p *Plugin) NewSession() references.Resolver {
	return &referenceResolverSession{resolver: p}
}

type pendingReferenceResolution struct {
	done      chan struct{}
	result    references.Result
	err       error
	abandoned bool
}

type referenceResolverSession struct {
	resolver references.Resolver
	mu       sync.Mutex
	cache    map[references.Request]references.Result
	pending  map[references.Request]*pendingReferenceResolution
}

func (s *referenceResolverSession) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	for {
		if err := ctx.Err(); err != nil {
			return references.Result{}, err
		}
		s.mu.Lock()
		if result, ok := s.cache[request]; ok {
			s.mu.Unlock()
			accounting.RecordResolverCall(ctx, nil, true)
			return result.Clone(), nil
		}
		pending, ok := s.pending[request]
		if !ok {
			break
		}
		s.mu.Unlock()
		if err := references.WaitOutsideLookupBudget(ctx, func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-pending.done:
				return ctx.Err()
			}
		}); err != nil {
			return references.Result{}, err
		}
		if !pending.abandoned {
			accounting.RecordResolverCall(ctx, nil, true)
			return pending.result.Clone(), pending.err
		}
	}
	if s.pending == nil {
		s.pending = make(map[references.Request]*pendingReferenceResolution)
	}
	pending := &pendingReferenceResolution{done: make(chan struct{})}
	s.pending[request] = pending
	s.mu.Unlock()
	result, err := s.resolver.Resolve(ctx, request)
	s.mu.Lock()
	pending.result, pending.err, pending.abandoned = result.Clone(), err, err != nil && ctx.Err() != nil
	if err == nil {
		if s.cache == nil {
			s.cache = make(map[references.Request]references.Result)
		}
		s.cache[request] = pending.result
	}
	delete(s.pending, request)
	close(pending.done)
	s.mu.Unlock()
	return result, err
}

type repositoryCallKey struct{}

type repositoryObjects struct {
	request  references.Request
	access   repository.Access
	active   bool
	opened   bool
	finished bool
	session  repository.Session
	identity repository.Identity
	refs     map[string]string
	objects  map[string]repository.Object
	fetchLog json.RawMessage
	err      error
}

func (r *repositoryObjects) fail(err error) error {
	if r.err == nil {
		r.err = err
	}
	return err
}

func (r *repositoryObjects) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return r.fail(err)
	}
	if !r.active {
		return r.fail(errors.New("repository objects are unavailable outside reference resolution"))
	}
	if r.err != nil {
		return r.err
	}
	if r.session == nil || r.finished {
		return r.fail(errors.New("repository lookup must be open and unfinished"))
	}
	return nil
}

func (r *repositoryObjects) open(ctx context.Context, name string, expectedID repositorybind.OptionU64) (repository.Identity, error) {
	if err := ctx.Err(); err != nil {
		return repository.Identity{}, r.fail(err)
	}
	if !r.active || r.opened || r.err != nil {
		return repository.Identity{}, r.fail(errors.New("repository lookup can be opened once during reference resolution"))
	}
	r.opened = true
	expected, hasExpected := r.request.ExpectedRepositoryID.Value()
	if name != r.request.Repository || expectedID.IsSome != hasExpected || hasExpected && expectedID.Value != expected {
		return repository.Identity{}, r.fail(errors.New("repository lookup does not match the resolution request"))
	}
	if r.access == nil {
		return repository.Identity{}, r.fail(errors.New("repository access is unavailable"))
	}
	session, err := r.access.Open(ctx, r.request)
	if err != nil {
		return repository.Identity{}, r.fail(err)
	}
	if session == nil {
		return repository.Identity{}, r.fail(errors.New("repository access returned no lookup"))
	}
	identity := session.Repository()
	if !strings.EqualFold(identity.Name, r.request.Repository) || identity.ID.Conflicts(r.request.ExpectedRepositoryID) {
		return repository.Identity{}, r.fail(errors.New("repository lookup returned a different repository"))
	}
	r.session, r.identity = session, identity
	r.refs, r.objects = make(map[string]string), make(map[string]repository.Object)
	return identity, nil
}

func (r *repositoryObjects) Open(ctx context.Context, name string, expectedID repositorybind.OptionU64) (repositorybind.ResultRepositoryString, error) {
	identity, err := r.open(ctx, name, expectedID)
	if err != nil {
		return repositorybind.ResultRepositoryStringErr{Value: r.fail(err).Error()}, nil
	}
	return repositorybind.ResultRepositoryStringOk{Value: repositorybind.Repository{Host: identity.Host, ID: lowerRepositoryID(identity.ID), Name: identity.Name, ObservedAt: identity.ObservedAt}}, nil
}

func (r *repositoryObjects) GetRef(ctx context.Context, name string) (repositorybind.ResultOptionStringString, error) {
	if err := r.ready(ctx); err != nil {
		return repositorybind.ResultOptionStringStringErr{Value: r.fail(err).Error()}, nil
	}
	value, found, err := r.session.Reference(ctx, name)
	if err != nil {
		return repositorybind.ResultOptionStringStringErr{Value: r.fail(err).Error()}, nil
	}
	if !found {
		return repositorybind.ResultOptionStringStringOk{}, nil
	}
	if !fullObjectID(value) {
		return repositorybind.ResultOptionStringStringErr{Value: r.fail(errors.New("repository returned an invalid reference object ID")).Error()}, nil
	}
	if previous, exists := r.refs[name]; exists && previous != value {
		return repositorybind.ResultOptionStringStringErr{Value: r.fail(errors.New("repository reference changed during resolution")).Error()}, nil
	}
	r.refs[name] = value
	return repositorybind.ResultOptionStringStringOk{Value: repositorybind.SomeString(value)}, nil
}

func (r *repositoryObjects) GetObject(ctx context.Context, id string) (repositorybind.ResultOptionObjectString, error) {
	if err := r.ready(ctx); err != nil {
		return repositorybind.ResultOptionObjectStringErr{Value: r.fail(err).Error()}, nil
	}
	if !fullObjectID(id) {
		return repositorybind.ResultOptionObjectStringErr{Value: r.fail(errors.New("repository object lookup requires a full lowercase object ID")).Error()}, nil
	}
	value, found, err := r.session.Object(ctx, id)
	if err != nil {
		return repositorybind.ResultOptionObjectStringErr{Value: r.fail(err).Error()}, nil
	}
	if !found {
		return repositorybind.ResultOptionObjectStringOk{}, nil
	}
	var object repositorybind.Object
	switch value := value.(type) {
	case repository.Commit:
		object = repositorybind.ObjectCommit{}
	case repository.Tree:
		object = repositorybind.ObjectTree{}
	case repository.PeeledTag:
		if !fullObjectID(value.Commit) {
			return repositorybind.ResultOptionObjectStringErr{Value: r.fail(errors.New("repository returned an invalid peeled commit ID")).Error()}, nil
		}
		object = repositorybind.ObjectPeeledTag{Value: value.Commit}
	case repository.PeeledTreeTag:
		if !fullObjectID(value.Tree) {
			return repositorybind.ResultOptionObjectStringErr{Value: r.fail(errors.New("repository returned an invalid peeled tree ID")).Error()}, nil
		}
		object = repositorybind.ObjectPeeledTreeTag{Value: value.Tree}
	default:
		return repositorybind.ResultOptionObjectStringErr{Value: r.fail(errors.New("repository returned an unknown object kind")).Error()}, nil
	}
	r.objects[id] = value
	return repositorybind.ResultOptionObjectStringOk{Value: repositorybind.SomeObject(object)}, nil
}

func (r *repositoryObjects) finish(ctx context.Context) error {
	if err := r.ready(ctx); err != nil {
		return err
	}
	r.finished = true
	log, err := r.session.Finish(ctx)
	if err != nil {
		return r.fail(err)
	}
	if !json.Valid(log) || string(log) == "null" {
		return r.fail(errors.New("repository lookup returned no fetch log"))
	}
	r.fetchLog = append(json.RawMessage(nil), log...)
	return nil
}

func (r *repositoryObjects) Finish(ctx context.Context) (repositorybind.Result_String, error) {
	if err := r.finish(ctx); err != nil {
		return repositorybind.Result_StringErr{Value: r.fail(err).Error()}, nil
	}
	return repositorybind.Result_StringOk{}, nil
}

func (r *repositoryObjects) validate(result references.Result) error {
	if r.err != nil {
		return fmt.Errorf("repository lookup failed: %w", r.err)
	}
	if !r.finished || r.session == nil || len(r.fetchLog) == 0 {
		return errors.New("reference resolver did not finish its repository lookup")
	}
	if result.RepositoryID != r.identity.ID || result.Repository != r.identity.Name || result.ObservedAt != r.identity.ObservedAt {
		return errors.New("reference resolver returned a different repository observation")
	}
	if !fullObjectID(result.SelectedObject) {
		return errors.New("reference resolver returned an invalid selected object ID")
	}
	if result.SelectedRef != "" {
		if object, found := r.refs[result.SelectedRef]; !found || object != result.SelectedObject {
			return errors.New("reference resolver selected a reference the host did not observe")
		}
	} else if result.SelectedObject != strings.ToLower(r.request.Ref) || !fullObjectID(strings.ToLower(r.request.Ref)) {
		return errors.New("reference resolver selected an object ID different from the request")
	}
	object, observed := r.objects[result.SelectedObject]
	if !observed {
		return errors.New("reference resolver selected an object the host did not observe")
	}
	var commit, tree string
	switch object := object.(type) {
	case repository.Commit:
		commit = result.SelectedObject
	case repository.Tree:
		tree = result.SelectedObject
	case repository.PeeledTag:
		commit = object.Commit
		if _, observed := r.objects[commit].(repository.Commit); !observed {
			return errors.New("reference resolver did not observe the peeled commit")
		}
	case repository.PeeledTreeTag:
		tree = object.Tree
		if _, observed := r.objects[tree].(repository.Tree); !observed {
			return errors.New("reference resolver did not observe the peeled tree")
		}
	}
	if result.Commit != commit || result.Tree != tree {
		return errors.New("reference resolver returned content inconsistent with the observed object")
	}
	return nil
}

func lowerRepositoryID(id repositoryid.ID) repositorybind.OptionU64 {
	if value, known := id.Value(); known {
		return repositorybind.SomeU64(value)
	}
	return repositorybind.NoneU64()
}

func fullObjectID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func lowerReferenceRequest(request references.Request) (wacogo.Val, error) {
	var kind uint32
	if request.Version != 1 || request.Repository == "" || request.Ref == "" {
		return nil, errors.New("unsupported or incomplete resolution request")
	}
	switch request.Kind {
	case "action":
		kind = 0
	case "workflow":
		kind = 1
	default:
		return nil, fmt.Errorf("unknown reference consumer %q", request.Kind)
	}
	expected := wacogo.ValOptionNone()
	if id, known := request.ExpectedRepositoryID.Value(); known {
		expected = wacogo.ValOptionSome(wacogo.ValU64(id))
	}
	return wacogo.NewValRecord(field("repository", wacogo.ValString(request.Repository)), field("reference", wacogo.ValString(request.Ref)), field("kind", wacogo.NewValEnum(kind)), field("expected-repository-id", expected)), nil
}

func liftReferenceResolution(value wacogo.Val) (references.Result, error) {
	outcome, ok := value.(*wacogo.ValResult)
	if !ok {
		return references.Result{}, liftError{"reference resolution", value}
	}
	if !outcome.IsOk() {
		message, ok := outcome.Err().(wacogo.ValString)
		if !ok {
			return references.Result{}, liftError{"reference resolution error", outcome.Err()}
		}
		return references.Result{}, fmt.Errorf("reference resolution failed: %s", message)
	}
	record, err := liftRecord(outcome.Ok(), "reference resolution")
	if err != nil {
		return references.Result{}, err
	}
	result := references.Result{Version: 1}
	identity, ok := record.Field("repository-id").(*wacogo.ValOption)
	if !ok {
		return result, liftError{"reference repository ID", record.Field("repository-id")}
	}
	if !identity.IsNone() {
		value, ok := identity.Val().(wacogo.ValU64)
		if !ok {
			return result, liftError{"reference repository ID", identity.Val()}
		}
		if result.RepositoryID, err = repositoryid.Known(uint64(value)); err != nil {
			return result, liftError{"reference repository ID", identity.Val()}
		}
	}
	for name, destination := range map[string]*string{"repository": &result.Repository, "observed-at": &result.ObservedAt, "selected-object": &result.SelectedObject} {
		if *destination, err = liftString(record, name); err != nil {
			return result, err
		}
	}
	selection, ok := record.Field("selection").(*wacogo.ValVariant)
	if !ok {
		return result, liftError{"reference selection", record.Field("selection")}
	}
	switch selection.Discriminant() {
	case 0:
		if selection.Val() != nil {
			return result, liftError{"object ID selection", selection.Val()}
		}
	case 1:
		name, ok := selection.Val().(wacogo.ValString)
		if !ok || name == "" {
			return result, liftError{"selected reference", selection.Val()}
		}
		result.SelectedRef = string(name)
	default:
		return result, liftError{"reference selection", selection}
	}
	content, ok := record.Field("content").(*wacogo.ValVariant)
	if !ok {
		return result, liftError{"reference content", record.Field("content")}
	}
	id, ok := content.Val().(wacogo.ValString)
	if !ok {
		return result, liftError{"reference content ID", content.Val()}
	}
	switch content.Discriminant() {
	case 0:
		result.Commit = string(id)
	case 1:
		result.Tree = string(id)
	default:
		return result, liftError{"reference content", content}
	}
	return result, nil
}
