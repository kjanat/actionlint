package security

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/outline"
)

// ResolvedReferenceCheck reports dependencies that provider resolves to a commit the
// reference does not already pin. It remembers the repository ID each
// repository first resolved to and rejects later results with another ID.
type ResolvedReferenceCheck struct {
	provider      references.Resolver
	mu            sync.Mutex
	repositoryIDs map[string]uint64
}

// NewResolvedReferenceCheck returns an analyzer that resolves dependencies through provider.
func NewResolvedReferenceCheck(provider references.Resolver) *ResolvedReferenceCheck {
	return &ResolvedReferenceCheck{provider: provider}
}

func (*ResolvedReferenceCheck) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "remote-resolution", Description: "Resolve action and workflow references with an explicit provider", Category: "security", Subcategory: "dependencies", Provider: "resolver", Maturity: "stable", Help: "Review the provider's resolved commit before pinning this dependency.", RequiredInputs: []checkinputs.InputKind{checkinputs.Dependencies}}
}

func (a *ResolvedReferenceCheck) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, dep := range input.Dependencies {
		target, ok := outline.ParseReference(dep.Uses).(outline.RepositoryReference)
		if !ok || target.HostSource != "default" {
			continue
		}
		name := strings.TrimSuffix(dep.Uses, "@"+target.Ref)
		ref := target.Ref
		request := references.Request{Version: 1, Repository: target.Owner + "/" + target.Repo, Ref: ref, Kind: dep.Kind}
		result, err := a.resolve(ctx, request)
		if err != nil {
			return findings, fmt.Errorf("resolve %s: %w", dep.Uses, err)
		}
		if result.Commit == "" || ref == result.Commit {
			continue
		}
		reference := dep.Reference
		d := results.Diagnostic{Rule: "remote-resolution", Code: "pin-available", Severity: "info", Reference: &reference, Path: input.Path, Start: dep.Start, End: dep.End, Message: fmt.Sprintf("%s resolves to commit %s (repository ID %d, observed %s)", dep.Uses, result.Commit, result.RepositoryID, result.ObservedAt)}
		if dep.End != dep.Start {
			d.Fixes = []results.Fix{{Description: "Pin " + name + " to resolved commit", Applicability: results.FixSuggested, Revisions: map[string]string{input.Path: input.Reference.Revision}, Edits: []results.Edit{{Path: input.Path, Start: dep.Start, End: dep.End, Replacement: strconv.Quote(name + "@" + result.Commit)}}}}
		}
		findings = append(findings, d)
	}
	return findings, nil
}

func (a *ResolvedReferenceCheck) resolve(ctx context.Context, request references.Request) (references.Result, error) {
	repository := strings.ToLower(request.Repository)
	a.mu.Lock()
	request.ExpectedRepositoryID = a.repositoryIDs[repository]
	a.mu.Unlock()
	result, err := a.provider.Resolve(ctx, request)
	if err != nil {
		return result, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Another document may have established the identity while this lookup ran.
	request.ExpectedRepositoryID = a.repositoryIDs[repository]
	if err := references.ValidateResult(request, result); err != nil {
		return result, err
	}
	if a.repositoryIDs == nil {
		a.repositoryIDs = make(map[string]uint64)
	}
	a.repositoryIDs[repository] = result.RepositoryID
	return result, nil
}
