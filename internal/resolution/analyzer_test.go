package resolution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/resolver"
	"actionlint.kjanat.dev/source"
)

type fakeResolver struct {
	requests []resolver.Request
	err      error
	tree     bool
	ids      []uint64
}

func (f *fakeResolver) Resolve(_ context.Context, r resolver.Request) (resolver.Result, error) {
	f.requests = append(f.requests, r)
	result := resolver.Result{Version: 1, Repository: r.Repository, Evidence: json.RawMessage(`{}`), RepositoryID: 42, ObservedAt: "2026-10-16T00:00:00Z"}
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
	rule := Analyzer{Resolver: provider}
	ref := source.Reference{Document: "workflow.yml", Revision: "revision", Node: "step", Address: "jobs.build.steps[0]"}
	input := analyzer.Input{Path: "workflow.yml", Reference: ref, Dependencies: []analyzer.Dependency{
		{Reference: ref, Uses: "owner/repo/path@v1", Kind: "action", Start: diagnostic.Position{Line: 1, Column: 1}, End: diagnostic.Position{Line: 1, Column: 24}},
		{Reference: ref, Uses: "owner/repo/.github/workflows/build.yml@v1", Kind: "workflow"},
		{Uses: "./local", Kind: "action"},
	}}
	findings, err := rule.Analyze(t.Context(), input)
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
	findings, err = rule.Analyze(t.Context(), input)
	if err != nil || len(findings) != 0 {
		t.Fatalf("tree became commit fix: %v %v", findings, err)
	}
}

func TestRepositoryIdentityAcrossDocuments(t *testing.T) {
	provider := &fakeResolver{ids: []uint64{42, 43, 43}}
	rule := Analyzer{Resolver: provider}
	first := analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: "Owner/Repo@v1", Kind: "action"}}}
	second := analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: "owner/repo/.github/workflows/build.yml@v2", Kind: "workflow"}}}
	if findings, err := rule.Analyze(t.Context(), first); err != nil || len(findings) != 1 {
		t.Fatalf("initial resolution failed: %v, %v", findings, err)
	}
	if findings, err := rule.Analyze(t.Context(), second); err == nil || len(findings) != 0 {
		t.Fatalf("repository identity changed: %v, %v", findings, err)
	}
	if provider.requests[0].ExpectedRepositoryID != 0 || provider.requests[1].ExpectedRepositoryID != 42 {
		t.Fatalf("repository identity not forwarded: %+v", provider.requests)
	}
	fresh := Analyzer{Resolver: provider}
	if findings, err := fresh.Analyze(t.Context(), second); err != nil || len(findings) != 1 || provider.requests[2].ExpectedRepositoryID != 0 {
		t.Fatalf("identity leaked into fresh session: %v, %v, %+v", findings, err, provider.requests)
	}
}

func TestRepositoryIdentityRetainsPartialFindings(t *testing.T) {
	provider := &fakeResolver{ids: []uint64{42, 43}}
	rule := Analyzer{Resolver: provider}
	input := analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: "Owner/Repo@v1", Kind: "action"}, {Uses: "owner/repo@v2", Kind: "action"}}}
	findings, err := rule.Analyze(t.Context(), input)
	if err == nil || len(findings) != 1 {
		t.Fatalf("inconsistent pins accepted or partial findings lost: %v, %v", findings, err)
	}
}

type concurrentIdentityResolver struct {
	started chan struct{}
	release chan struct{}
	count   atomic.Uint64
}

func (r *concurrentIdentityResolver) Resolve(ctx context.Context, request resolver.Request) (resolver.Result, error) {
	id := r.count.Add(1)
	r.started <- struct{}{}
	select {
	case <-r.release:
		return resolver.Result{Version: 1, Repository: request.Repository, RepositoryID: id, Commit: strings.Repeat("a", 40), ObservedAt: "2026-10-16T00:00:00Z", Evidence: json.RawMessage(`{}`)}, nil
	case <-ctx.Done():
		return resolver.Result{}, ctx.Err()
	}
}

func TestRepositoryIdentityConcurrentFirstResults(t *testing.T) {
	provider := &concurrentIdentityResolver{started: make(chan struct{}, 2), release: make(chan struct{})}
	rule := Analyzer{Resolver: provider}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		findings []diagnostic.Diagnostic
		err      error
	}
	results := make(chan outcome, 2)
	for _, uses := range []string{"Owner/Repo@v1", "owner/repo@v2"} {
		go func() {
			findings, err := rule.Analyze(ctx, analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: uses, Kind: "action"}}})
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
			rule := Analyzer{Resolver: provider}
			findings, err := rule.Analyze(t.Context(), analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: uses, Kind: "action"}}})
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
			rule := Analyzer{Resolver: provider}
			uses := "Owner/Repo/.github/workflows/build.yml@feature/release"
			findings, err := rule.Analyze(t.Context(), analyzer.Input{Dependencies: []analyzer.Dependency{{Uses: uses, Kind: kind}}})
			if err != nil || len(findings) != 1 || len(provider.requests) != 1 {
				t.Fatalf("valid reference not resolved: %v %v %+v", findings, err, provider.requests)
			}
			want := resolver.Request{Version: 1, Repository: "Owner/Repo", Ref: "feature/release", Kind: kind}
			if provider.requests[0] != want {
				t.Fatalf("request = %+v, want %+v", provider.requests[0], want)
			}
		})
	}
}
