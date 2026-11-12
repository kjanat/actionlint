package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path"
	"strings"
	"sync"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

// Provider obtains a reusable workflow at a resolved repository commit.
// Missing or inaccessible files return unavailable data; cancellation returns an error.
type Provider interface {
	LookupWorkflow(context.Context, string) (RemoteFile, error)
}

// RemoteFile stores the requested reference, resolved commit and immutable YAML.
type RemoteFile struct {
	spec, commit, content string
	missing               availability.Unavailability
}

func IsRemoteReference(spec string) bool {
	if syntax.ContainsExpression(spec) {
		return false
	}
	ref, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return false
	}
	return path.Dir(ref.Directory()) == ".github/workflows" && (path.Ext(ref.Directory()) == ".yml" || path.Ext(ref.Directory()) == ".yaml")
}

func NewRemoteFile(spec, commit string, content []byte) (RemoteFile, error) {
	if !IsRemoteReference(spec) || (len(commit) != 40 && len(commit) != 64) {
		return RemoteFile{}, errors.New("invalid remote workflow reference or commit")
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return RemoteFile{}, errors.New("invalid remote workflow commit")
	}
	ref, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return RemoteFile{}, err
	}
	if requested, err := hex.DecodeString(ref.Ref()); err == nil && (len(requested) == 20 || len(requested) == 32) && !strings.EqualFold(ref.Ref(), commit) {
		return RemoteFile{}, errors.New("remote workflow commit differs from the requested object")
	}
	return RemoteFile{spec: spec, commit: strings.ToLower(commit), content: string(content)}, nil
}

func UnavailableRemoteFile(reason availability.Unavailability) RemoteFile {
	return RemoteFile{missing: reason}
}
func (f RemoteFile) Available() bool             { return f.commit != "" }
func (f RemoteFile) Commit() string              { return f.commit }
func (f RemoteFile) Content() string             { return f.content }
func (f RemoteFile) Reason() availability.Reason { return f.missing.Reason() }

// Remote shares fetched and parsed workflow metadata across repository caches
// in one analysis. Lookup never starts a network request.
type Remote struct {
	provider Provider
	mu       sync.Mutex
	results  map[string]reusableWorkflowMetadataResult
	pending  map[string]chan struct{}
	contents map[string][32]byte
}

func NewRemote(provider Provider) *Remote {
	return &Remote{provider: provider, results: make(map[string]reusableWorkflowMetadataResult), pending: make(map[string]chan struct{}), contents: make(map[string][32]byte)}
}

func (r *Remote) Lookup(spec string) (*calls.Metadata, error) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	result := r.results[spec]
	r.mu.Unlock()
	if result.failure.failed() {
		return nil, result.failure
	}
	return result.metadata.Clone(), nil
}

func (r *Remote) Load(ctx context.Context, spec string) (*calls.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.provider == nil || !IsRemoteReference(spec) {
		return nil, nil
	}
	r.mu.Lock()
	if _, exists := r.results[spec]; exists {
		r.mu.Unlock()
		return r.Lookup(spec)
	}
	if ready, exists := r.pending[spec]; exists {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready:
			return r.Load(ctx, spec)
		}
	}
	ready := make(chan struct{})
	r.pending[spec] = ready
	r.mu.Unlock()
	file, err := r.provider.LookupWorkflow(ctx, spec)
	if err == nil {
		err = ctx.Err()
	}
	result := reusableWorkflowMetadataResult{}
	if err == nil && file.Available() {
		if file.spec != spec {
			err = errors.New("remote workflow provider returned a different reference")
		} else {
			result.metadata, err = calls.ParseMetadata([]byte(file.content))
			if err != nil {
				result.failure = parseFailure(spec, err)
				err = nil
			}
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	r.mu.Lock()
	if err == nil && file.Available() {
		ref, parseErr := actions.ParseReference("github.com", spec)
		if parseErr != nil {
			err = parseErr
		} else {
			key := strings.ToLower(ref.Repository()) + "\x00" + ref.Directory() + "\x00" + file.commit
			digest := sha256.Sum256([]byte(file.content))
			if previous, exists := r.contents[key]; exists && previous != digest {
				err = errors.New("remote workflow provider changed content for one commit and path")
			} else {
				r.contents[key] = digest
			}
		}
	}
	if err == nil {
		r.results[spec] = result
	}
	delete(r.pending, spec)
	close(ready)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return r.Lookup(spec)
}
