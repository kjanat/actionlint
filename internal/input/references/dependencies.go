package references

import (
	"context"
	"fmt"
	"strings"
	"sync"

	callrefs "actionlint.kjanat.dev/v2/internal/model/calls/references"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
)

// ResolutionSession acquires dependency targets and validates repository identity
// across all documents in one analysis. A new analysis needs a new session.
type ResolutionSession struct {
	provider      Resolver
	mu            sync.Mutex
	repositoryIDs map[string]repositoryid.ID
}

func NewResolutionSession(provider Resolver) *ResolutionSession {
	return &ResolutionSession{provider: provider}
}

// ResolveDependencies returns successful observations before any failure.
// Unsupported reference forms do not initiate provider requests.
func (session *ResolutionSession) ResolveDependencies(ctx context.Context, dependencies []workflowfacts.Dependency) ([]workflowfacts.ResolvedDependency, error) {
	var resolved []workflowfacts.ResolvedDependency
	for _, dependency := range dependencies {
		if err := ctx.Err(); err != nil {
			return resolved, err
		}
		target, ok := callrefs.ParseRepositoryUses(dependency.Uses)
		if !ok {
			continue
		}
		request := Request{Version: 1, Repository: target.Owner + "/" + target.Repo, Ref: target.Ref, Kind: dependency.Kind}
		result, err := session.resolve(ctx, request)
		if err != nil {
			return resolved, fmt.Errorf("resolve %s: %w", dependency.Uses, err)
		}
		resolved = append(resolved, workflowfacts.ResolvedDependency{
			Dependency: dependency, Ref: request.Ref,
			RepositoryID: result.RepositoryID, Commit: result.Commit, Tree: result.Tree, ObservedAt: result.ObservedAt,
		})
	}
	return resolved, nil
}

func (session *ResolutionSession) resolve(ctx context.Context, request Request) (Result, error) {
	repository := strings.ToLower(request.Repository)
	session.mu.Lock()
	request.ExpectedRepositoryID = session.repositoryIDs[repository]
	session.mu.Unlock()
	result, err := session.provider.Resolve(ctx, request)
	if err != nil {
		return result, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	// A concurrent lookup may have established the identity after this request.
	request.ExpectedRepositoryID = session.repositoryIDs[repository]
	if err := ValidateResult(request, result); err != nil {
		return result, err
	}
	if result.RepositoryID.IsZero() {
		return result, nil
	}
	if session.repositoryIDs == nil {
		session.repositoryIDs = make(map[string]repositoryid.ID)
	}
	session.repositoryIDs[repository] = result.RepositoryID
	return result, nil
}
