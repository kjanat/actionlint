package references_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	"actionlint.kjanat.dev/v2/internal/checking/rules/security"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/references"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/model/repositoryid"
	workflowfacts "actionlint.kjanat.dev/v2/internal/model/workflows/facts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type fakeResolver struct {
	requests []references.Request
	err      error
	tree     bool
	ids      []repositoryid.ID
}

func knownRepositoryID(value uint64) repositoryid.ID {
	id, err := repositoryid.Known(value)
	if err != nil {
		panic(err)
	}
	return id
}

func TestCheckingSessionPreparesResolverFacts(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "complete"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			provider := &fakeResolver{}
			wantRequests := 1
			steps := "      - uses: owner/repo@v1\n"
			if partial {
				wantRequests = 2
				provider.ids = []repositoryid.ID{knownRepositoryID(42), knownRepositoryID(43)}
				steps += "      - uses: owner/repo@v2\n"
			}
			content := "on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n" + steps
			result, err := checking.Check(t.Context(), checking.Options{WorkingDir: t.TempDir(), SkipProjectConfig: true, Resolver: provider, SuggestCommitPins: true}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(content)})
			if (err != nil) != partial || result == nil {
				t.Fatalf("wrong session outcome: %v", err)
			}
			count := 0
			for _, finding := range result.Diagnostics {
				if finding.Rule != "remote-resolution" {
					continue
				}
				count++
				if finding.Reference == nil || finding.Reference.Revision == "" || len(finding.Fixes) != 1 {
					t.Fatalf("resolution lost captured identity or fix: %+v", finding)
				}
			}
			if count != 1 || len(provider.requests) != wantRequests {
				t.Fatalf("prepared resolutions not checked: count=%d requests=%v error=%v", count, provider.requests, err)
			}
		})
	}
}

func TestDisabledResolutionDoesNotCallProvider(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "actionlint.yml")
	if err := os.WriteFile(config, []byte("lint: {rules: {security: {remote-resolution: off}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeResolver{err: errors.New("disabled resolution called provider")}
	content := "on: push\npermissions: {}\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: owner/repo@v1}]}}\n"
	_, err := checking.Check(t.Context(), checking.Options{WorkingDir: root, ConfigFile: config, Resolver: provider, SuggestCommitPins: true}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(content)})
	if err != nil || len(provider.requests) != 0 {
		t.Fatalf("disabled resolution acquired input: requests=%v error=%v", provider.requests, err)
	}
}

func (f *fakeResolver) Resolve(_ context.Context, r references.Request) (references.Result, error) {
	f.requests = append(f.requests, r)
	result := references.Result{Version: 1, Repository: r.Repository, FetchLog: json.RawMessage(`{}`), RepositoryID: knownRepositoryID(42), ObservedAt: "2026-10-16T00:00:00Z"}
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
	rule := newReferenceCheck(provider)
	ref := sourcepos.Reference{Document: "workflow.yml", Revision: "revision", Node: "step", Address: "jobs.build.steps[0]"}
	input := checkinputs.Input{Path: "workflow.yml", Reference: ref, Dependencies: []workflowfacts.Dependency{
		{Reference: ref, Uses: "owner/repo/path@v1", Kind: "action", Start: sourcepos.Position{Line: 1, Column: 1}, End: sourcepos.Position{Line: 1, Column: 24}},
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
	provider := &fakeResolver{ids: []repositoryid.ID{knownRepositoryID(42), knownRepositoryID(43), knownRepositoryID(43)}}
	rule := newReferenceCheck(provider)
	first := checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: "Owner/Repo@v1", Kind: "action"}}}
	second := checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: "owner/repo/.github/workflows/build.yml@v2", Kind: "workflow"}}}
	if findings, err := rule.CheckDocument(t.Context(), first); err != nil || len(findings) != 1 {
		t.Fatalf("initial resolution failed: %v, %v", findings, err)
	}
	if findings, err := rule.CheckDocument(t.Context(), second); err == nil || len(findings) != 0 {
		t.Fatalf("repository identity changed: %v, %v", findings, err)
	}
	if !provider.requests[0].ExpectedRepositoryID.IsZero() || provider.requests[1].ExpectedRepositoryID != knownRepositoryID(42) {
		t.Fatalf("repository identity not forwarded: %+v", provider.requests)
	}
	fresh := newReferenceCheck(provider)
	if findings, err := fresh.CheckDocument(t.Context(), second); err != nil || len(findings) != 1 || !provider.requests[2].ExpectedRepositoryID.IsZero() {
		t.Fatalf("identity leaked into fresh session: %v, %v, %+v", findings, err, provider.requests)
	}
}

func TestUnknownRepositoryIDsAreComparedByName(t *testing.T) {
	provider := &fakeResolver{ids: []repositoryid.ID{{}, knownRepositoryID(42), {}, knownRepositoryID(42)}}
	rule := newReferenceCheck(provider)
	for _, uses := range []string{"Owner/Repo@v1", "owner/repo@v2", "owner/repo@v3", "OWNER/REPO@v4"} {
		findings, err := rule.CheckDocument(t.Context(), checkinputs.Input{Dependencies: []workflowfacts.Dependency{{Uses: uses, Kind: "action"}}})
		if err != nil || len(findings) != 1 {
			t.Fatalf("%s: %v, %v", uses, findings, err)
		}
		if uses == "Owner/Repo@v1" && findings[0].Message != "Owner/Repo@v1 resolves to commit "+strings.Repeat("a", 40)+" (observed 2026-10-16T00:00:00Z)" {
			t.Fatalf("message without repository ID = %q", findings[0].Message)
		}
	}
	want := []repositoryid.ID{{}, {}, knownRepositoryID(42), knownRepositoryID(42)}
	for i, request := range provider.requests {
		if request.ExpectedRepositoryID != want[i] {
			t.Fatalf("request %d expected repository ID = %+v, want %+v", i, request.ExpectedRepositoryID, want[i])
		}
	}
}

func TestRepositoryIdentityRetainsPartialFindings(t *testing.T) {
	provider := &fakeResolver{ids: []repositoryid.ID{knownRepositoryID(42), knownRepositoryID(43)}}
	rule := newReferenceCheck(provider)
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
		return references.Result{Version: 1, Repository: request.Repository, RepositoryID: knownRepositoryID(id), Commit: strings.Repeat("a", 40), ObservedAt: "2026-10-16T00:00:00Z", FetchLog: json.RawMessage(`{}`)}, nil
	case <-ctx.Done():
		return references.Result{}, ctx.Err()
	}
}

func TestRepositoryIdentityConcurrentFirstResults(t *testing.T) {
	provider := &concurrentIdentityResolver{started: make(chan struct{}, 2), release: make(chan struct{})}
	rule := newReferenceCheck(provider)
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
			rule := newReferenceCheck(provider)
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
			rule := newReferenceCheck(provider)
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

// referenceCheck composes input acquisition and the pure rule for integration tests.
type referenceCheck struct {
	session *references.ResolutionSession
	rule    checkinputs.Checker
}

func newReferenceCheck(provider references.Resolver) referenceCheck {
	return referenceCheck{session: references.NewResolutionSession(provider), rule: security.NewResolvedReferenceCheck()}
}

func (check referenceCheck) CheckDocument(ctx context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	resolved, preparationErr := check.session.ResolveDependencies(ctx, input.Dependencies)
	input.ResolvedDependencies = resolved
	findings, checkErr := check.rule.CheckDocument(ctx, input)
	return findings, errors.Join(preparationErr, checkErr)
}
