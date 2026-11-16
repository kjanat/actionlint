package workflows

import (
	"context"
	"encoding/hex"
	"errors"
	"path"
	"strings"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/remotecontent"
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
	store    *remotecontent.Store[string, reusableWorkflowMetadataResult, parsedWorkflow]
}

// parsedWorkflow is the outcome of parsing one immutable workflow file.
type parsedWorkflow struct {
	metadata *calls.Metadata
	err      error
}

func parseWorkflow(content []byte) parsedWorkflow {
	metadata, err := calls.ParseMetadata(content)
	return parsedWorkflow{metadata: metadata, err: err}
}

// resultFor names a parse failure with the requested reference.
func (p parsedWorkflow) resultFor(spec string) reusableWorkflowMetadataResult {
	if p.err != nil {
		return reusableWorkflowMetadataResult{failure: parseFailure(spec, p.err)}
	}
	return reusableWorkflowMetadataResult{metadata: p.metadata}
}

func NewRemote(provider Provider) *Remote {
	return &Remote{provider: provider, store: remotecontent.New[string, reusableWorkflowMetadataResult](parseWorkflow)}
}

func (r *Remote) Lookup(spec string) (*calls.Metadata, error) {
	if r == nil {
		return nil, nil
	}
	result, _ := r.store.Lookup(spec)
	return result.outcome()
}

func (r *Remote) LoadWorkflowMetadata(ctx context.Context, spec string) (*calls.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.provider == nil || !IsRemoteReference(spec) {
		return nil, nil
	}
	result, err := r.store.Acquire(ctx, spec, func(ctx context.Context) (reusableWorkflowMetadataResult, error) {
		return r.fetch(ctx, spec)
	})
	if err != nil {
		return nil, err
	}
	return result.outcome()
}

func (r *Remote) fetch(ctx context.Context, spec string) (reusableWorkflowMetadataResult, error) {
	file, err := r.provider.LookupWorkflow(ctx, spec)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil || !file.Available() {
		return reusableWorkflowMetadataResult{}, err
	}
	if file.spec != spec {
		return reusableWorkflowMetadataResult{}, errors.New("remote workflow provider returned a different reference")
	}
	ref, err := actions.ParseReference("github.com", spec)
	if err != nil {
		return reusableWorkflowMetadataResult{}, err
	}
	parsed, unchanged := r.store.Parse(remotecontent.Content{
		Location: remotecontent.Location{Host: ref.Host(), Repository: ref.Repository(), Commit: file.commit, Path: ref.Directory()},
		Bytes:    file.content,
	})
	if !unchanged {
		return reusableWorkflowMetadataResult{}, errors.New("remote workflow provider changed content for one commit and path")
	}
	if err := ctx.Err(); err != nil {
		return reusableWorkflowMetadataResult{}, err
	}
	return parsed.resultFor(spec), nil
}
