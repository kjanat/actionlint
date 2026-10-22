package resolution

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/document"
	"actionlint.kjanat.dev/resolver"
)

type Analyzer struct {
	Resolver      resolver.Resolver
	mu            sync.Mutex
	repositoryIDs map[string]uint64
}

func (*Analyzer) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "remote-resolution", Description: "Resolve action and workflow references with an explicit provider", Category: "security", Subcategory: "dependencies", Provider: "resolver", Maturity: "stable", FixAvailability: "suggested", Help: "Review the provider's resolved commit before pinning this dependency.", Requires: []analyzer.Fact{analyzer.Dependencies}}
}

func (a *Analyzer) Analyze(ctx context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	var findings []diagnostic.Diagnostic
	for _, dep := range input.Dependencies {
		target, ok := document.ParseReference(dep.Uses).(document.RepositoryReference)
		if !ok || target.HostSource != "default" {
			continue
		}
		name := strings.TrimSuffix(dep.Uses, "@"+target.Ref)
		ref := target.Ref
		request := resolver.Request{Version: 1, Repository: target.Owner + "/" + target.Repo, Ref: ref, Kind: dep.Kind}
		result, err := a.resolve(ctx, request)
		if err != nil {
			return findings, fmt.Errorf("resolve %s: %w", dep.Uses, err)
		}
		if result.Commit == "" || ref == result.Commit {
			continue
		}
		reference := dep.Reference
		d := diagnostic.Diagnostic{Rule: "remote-resolution", Code: "pin-available", Severity: "info", Reference: &reference, Path: input.Path, Start: dep.Start, End: dep.End, Message: fmt.Sprintf("%s resolves to commit %s (repository ID %d, observed %s)", dep.Uses, result.Commit, result.RepositoryID, result.ObservedAt)}
		if dep.End != dep.Start {
			d.Fixes = []diagnostic.Fix{{Description: "Pin " + name + " to resolved commit", Applicability: "suggested", Revisions: map[string]string{input.Path: input.Reference.Revision}, Edits: []diagnostic.Edit{{Path: input.Path, Start: dep.Start, End: dep.End, Replacement: strconv.Quote(name + "@" + result.Commit)}}}}
		}
		findings = append(findings, d)
	}
	return findings, nil
}

func (a *Analyzer) resolve(ctx context.Context, request resolver.Request) (resolver.Result, error) {
	repository := strings.ToLower(request.Repository)
	a.mu.Lock()
	request.ExpectedRepositoryID = a.repositoryIDs[repository]
	a.mu.Unlock()
	result, err := a.Resolver.Resolve(ctx, request)
	if err != nil {
		return result, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// Another document may have established the identity while this lookup ran.
	request.ExpectedRepositoryID = a.repositoryIDs[repository]
	if err := validateResult(request, result); err != nil {
		return result, err
	}
	if a.repositoryIDs == nil {
		a.repositoryIDs = make(map[string]uint64)
	}
	a.repositoryIDs[repository] = result.RepositoryID
	return result, nil
}
