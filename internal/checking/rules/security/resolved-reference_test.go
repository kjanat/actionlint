package security

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/input/references"
	"actionlint.kjanat.dev/v2/internal/input/source"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type fakeResolver struct {
	requests []references.Request
	err      error
	tree     bool
	ids      []uint64
}

func (f *fakeResolver) Resolve(_ context.Context, r references.Request) (references.Result, error) {
	f.requests = append(f.requests, r)
	result := references.Result{Version: 1, Repository: r.Repository, FetchLog: json.RawMessage(`{}`), RepositoryID: 42, ObservedAt: "2026-10-16T00:00:00Z"}
	if len(f.ids) >= len(f.requests) {
		result.RepositoryID = f.ids[len(f.requests)-1]
	}
	if !f.tree {
		result.Commit = strings.Repeat("a", 40)
	} else {
		result.Tree = strings.Repeat("b", 40)
	}
	return result, f.err
}

func TestConsumerAndPinProvenance(t *testing.T) {
	provider := &fakeResolver{}
	rule := NewResolvedReferenceCheck(provider)
	ref := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "step", Address: "jobs.build.steps[0]"}
	input := checkinputs.Input{Path: "workflow.yml", Reference: ref, Dependencies: []workflowfacts.Dependency{
		{Reference: ref, Uses: "owner/repo/path@v1", Kind: "action", Start: results.Position{Line: 1, Column: 1}, End: results.Position{Line: 1, Column: 24}},
		{Reference: ref, Uses: "owner/repo/.github/workflows/build.yml@v1", Kind: "workflow"},
		{Uses: "./local", Kind: "action"},
	}}
	findings, err := rule.CheckDocument(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || provider.requests[0].Kind != "action" || provider.requests[1].Kind != "workflow" {
		t.Fatal(provider.requests)
	}
	if len(findings) != 2 || findings[0].Reference.Node != "step" || findings[0].Fixes[0].Revisions[input.Path] != "revision" || !strings.Contains(findings[0].Fixes[0].Edits[0].Replacement, "owner/repo/path@"+strings.Repeat("a", 40)) {
		t.Fatal(findings)
	}
	provider.tree = true
	findings, err = rule.CheckDocument(t.Context(), input)
	if err != nil || len(findings) != 0 {
		t.Fatalf("tree became commit fix: %v %v", findings, err)
	}
}

func TestRepositoryIdentityAcrossDocuments(t *testing.T) {
	provider := &fakeResolver{ids: []uint64{42, 43, 43}}
	rule := NewResolvedReferenceCheck(provider)
	first := checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: "Owner/Repo@v1", Kind: "action"}}}
	second := checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: "owner/repo/.github/workflows/build.yml@v2", Kind: "workflow"}}}
	if findings, err := rule.CheckDocument(t.Context(), first); err != nil || len(findings) != 1 {
		t.Fatalf("initial resolution failed: %v, %v", findings, err)
	}
	if findings, err := rule.CheckDocument(t.Context(), second); err == nil || len(findings) != 0 {
		t.Fatalf("repository identity changed: %v, %v", findings, err)
	}
	if provider.requests[0].ExpectedRepositoryID != 0 || provider.requests[1].ExpectedRepositoryID != 42 {
		t.Fatalf("repository identity not forwarded: %+v", provider.requests)
	}
	fresh := NewResolvedReferenceCheck(provider)
	if findings, err := fresh.CheckDocument(t.Context(), second); err != nil || len(findings) != 1 || provider.requests[2].ExpectedRepositoryID != 0 {
		t.Fatalf("identity leaked into fresh session: %v, %v, %+v", findings, err, provider.requests)
	}
}

func TestRepositoryIdentityRetainsPartialFindings(t *testing.T) {
	provider := &fakeResolver{ids: []uint64{42, 43}}
	rule := NewResolvedReferenceCheck(provider)
	input := checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: "Owner/Repo@v1", Kind: "action"}, {Uses: "owner/repo@v2", Kind: "action"}}}
	findings, err := rule.CheckDocument(t.Context(), input)
	if err == nil || len(findings) != 1 {
		t.Fatalf("inconsistent pins accepted or partial findings lost: %v, %v", findings, err)
	}
}

type concurrentIdentityResolver struct {
	started chan struct{}
	release chan struct{}
	count   atomic.Uint64
}

func (r *concurrentIdentityResolver) Resolve(ctx context.Context, request references.Request) (references.Result, error) {
	id := r.count.Add(1)
	r.started <- struct{}{}
	select {
	case <-r.release:
		return references.Result{Version: 1, Repository: request.Repository, RepositoryID: id, Commit: strings.Repeat("a", 40), ObservedAt: "2026-10-16T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
	case <-ctx.Done():
		return references.Result{}, ctx.Err()
	}
}

func TestRepositoryIdentityConcurrentFirstResults(t *testing.T) {
	provider := &concurrentIdentityResolver{started: make(chan struct{}, 2), release: make(chan struct{})}
	rule := NewResolvedReferenceCheck(provider)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		findings []results.Diagnostic
		err      error
	}
	results := make(chan outcome, 2)
	for _, uses := range []string{"Owner/Repo@v1", "owner/repo@v2"} {
		go func() {
			findings, err := rule.CheckDocument(ctx, checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: uses, Kind: "action"}}})
			results <- outcome{findings: findings, err: err}
		}()
	}
	for range 2 {
		select {
		case <-provider.started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(provider.release)
	succeeded, rejected, findings := 0, 0, 0
	for range 2 {
		result := <-results
		findings += len(result.findings)
		if result.err == nil {
			succeeded++
		} else {
			rejected++
		}
	}
	if succeeded != 1 || rejected != 1 || findings != 1 {
		t.Fatalf("concurrent identities mixed: successes=%d rejected=%d findings=%d", succeeded, rejected, findings)
	}
}

func TestConsumerSkipsUnsupportedReferences(t *testing.T) {
	for _, uses := range []string{
		"owner/@v1", "https://host/owner/repo@v1", "owner/repo@", "/repo@v1",
		"owner/repo", "owner//repo@v1", "owner/repo/../action@v1", "owner/repo@${{ inputs.ref }}",
		"./local", "$/action@v1", "docker://alpine:3", "self:owner/repo@v1",
	} {
		t.Run(uses, func(t *testing.T) {
			provider := &fakeResolver{err: errors.New("unsupported reference reached provider")}
			rule := NewResolvedReferenceCheck(provider)
			findings, err := rule.CheckDocument(t.Context(), checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: uses, Kind: "action"}}})
			if err != nil || len(findings) != 0 || len(provider.requests) != 0 {
				t.Fatalf("unsupported reference resolved: requests=%v findings=%v error=%v", provider.requests, findings, err)
			}
		})
	}
}

func TestConsumerPreservesSubpathAndRef(t *testing.T) {
	for _, kind := range []string{"action", "workflow"} {
		t.Run(kind, func(t *testing.T) {
			provider := &fakeResolver{}
			rule := NewResolvedReferenceCheck(provider)
			uses := "Owner/Repo/.github/workflows/build.yml@feature/release"
			findings, err := rule.CheckDocument(t.Context(), checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: uses, Kind: kind}}})
			if err != nil || len(findings) != 1 || len(provider.requests) != 1 {
				t.Fatalf("valid reference not resolved: %v %v %+v", findings, err, provider.requests)
			}
			want := references.Request{Version: 1, Repository: "Owner/Repo", Ref: "feature/release", Kind: kind}
			if provider.requests[0] != want {
				t.Fatalf("request = %+v, want %+v", provider.requests[0], want)
			}
		})
	}
}
