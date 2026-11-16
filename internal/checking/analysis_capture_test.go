package checking

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/registry"
	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture"
	"actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"

	"actionlint.kjanat.dev/v2/internal/model/workflows"
	"actionlint.kjanat.dev/v2/internal/results"

	"actionlint.kjanat.dev/v2/internal/results/finding"

	"actionlint.kjanat.dev/v2/internal/results/outline"
)

func TestSourceCaptureJoinCancelsAndRetriesBeforePublication(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		lifetime, cancel := context.WithCancel(t.Context())
		defer cancel()
		started := make(chan struct{})
		store, err := capture.New(lifetime, nil, func(ctx context.Context, _ string) ([]byte, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		capturefixture.CloseCaptures(t, store)
		returned := make(chan error, 1)
		go func() {
			_, err := store.Read(t.Context(), filepath.Join(t.TempDir(), "pending.yml"), source.YAML)
			returned <- err
		}()
		<-started
		sealed, err := joinSourceCaptures(t.Context(), store, cancel, 5*time.Millisecond, false)
		_, failedJoin := errors.AsType[*capture.JoinError](err)
		if !sealed || !failedJoin || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("join did not report the interrupted first attempt: sealed=%t error=%v", sealed, err)
		}
		if err := <-returned; !errors.Is(err, context.Canceled) {
			t.Fatalf("retry did not cancel backing IO: %v", err)
		}
		rows, err := store.Sources()
		if err != nil || len(rows) != 1 || rows[0].State != capture.Failed || !errors.Is(rows[0].Error, context.Canceled) {
			t.Fatalf("retry returned before sealing cancelled capture: %#v, %v", rows, err)
		}
	})
}

func TestSourceCapturePersistentJoinFailureBlocksPublication(t *testing.T) {
	t.Parallel()
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	releaseReader := sync.OnceFunc(func() { close(release) })
	defer releaseReader()
	store, err := capture.New(lifetime, nil, func(context.Context, string) ([]byte, error) {
		close(started)
		<-release
		return []byte("ignores cancellation"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	returned := make(chan error, 1)
	path := filepath.Join(t.TempDir(), "pending.yml")
	go func() { _, err := store.Read(t.Context(), path, source.YAML); returned <- err }()
	<-started
	sealed, err := joinSourceCaptures(t.Context(), store, cancel, 5*time.Millisecond, false)
	var attempts interface{ Unwrap() []error }
	if sealed || !errors.As(err, &attempts) || len(attempts.Unwrap()) != 2 {
		t.Fatalf("persistent join failure allowed publication: sealed=%t error=%v", sealed, err)
	}
	for _, attempt := range attempts.Unwrap() {
		if _, failedJoin := errors.AsType[*capture.JoinError](attempt); !failedJoin || !errors.Is(attempt, context.DeadlineExceeded) {
			t.Fatalf("join attempt lost its failure: %v", attempt)
		}
	}
	if _, err := store.Sources(); !errors.Is(err, capture.ErrNotSealed) {
		t.Fatalf("unjoined sources became publishable: %v", err)
	}
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	releaseReader()
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

type capturedTraversal struct {
	workflowvisit.Base
	mu        *sync.Mutex
	workflows *[]workflows.Step
}

func (r *capturedTraversal) VisitWorkflowPre(workflow workflows.Workflow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range workflow.Jobs() {
		for step := range job.Steps() {
			*r.workflows = append(*r.workflows, step)
			return nil
		}
	}
	return nil
}

func TestDuplicateEntriesTraverseOneCanonicalCapturedAST(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var workflows []workflows.Step
	content := []byte(commandGoodWorkflow)
	var toolCalls atomic.Int32
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: t.TempDir(),
		Sources:    []sourceUnit{{Path: "workflow.yml", Content: content}, {Path: "workflow.yml", Content: content}},
		Tools:      registry.Registrations(registry.Selections{"shellcheck": {Runner: func([]string, string) ([]byte, error) { toolCalls.Add(1); return []byte(`{"comments":[]}`), nil }}}),
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			if len(rules) == 0 {
				t.Error("default rules were not built")
			}
			return append(rules, &capturedTraversal{Base: workflowvisit.NewBase("capture-traversal", ""), mu: &mu, workflows: &workflows})
		},
	})
	if err != nil || result == nil {
		t.Fatalf("duplicate entry analysis failed: %v", err)
	}
	if len(workflows) != 2 || workflows[0] != workflows[1] {
		t.Fatalf("entries traversed independently decoded ASTs: %p", workflows)
	}
	if toolCalls.Load() != 2 {
		t.Fatalf("script tools did not analyze both shared entries: %d", toolCalls.Load())
	}
}

type sharedCompositeObserver struct {
	workflowvisit.Base
	mu        *sync.Mutex
	snapshots *[]parsing.SourceSnapshot
	steps     *[]workflows.Step
}

func (r *sharedCompositeObserver) EnterInvocation(scope workflowvisit.Invocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.snapshots = append(*r.snapshots, scope.Source)
	return nil
}

func (r *sharedCompositeObserver) VisitInvocationStep(step workflows.Step) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.steps = append(*r.steps, step)
	return nil
}
func (*sharedCompositeObserver) VisitOpaqueAction(workflows.Step) error   { return nil }
func (*sharedCompositeObserver) LeaveInvocation(invocation.Context) error { return nil }

func TestDefaultRulesAndScriptToolsShareCompositeCapturedAST(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	actionPath := filepath.Join(root, "local", "action.yml")
	if err := os.MkdirAll(filepath.Dir(actionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actionPath, []byte("name: shared\ndescription: shared\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo '${{ missing.value }}'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/local}]}}\n")
	var mu sync.Mutex
	var snapshots []parsing.SourceSnapshot
	var steps []workflows.Step
	var toolCalls atomic.Int32
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources:    []sourceUnit{{Path: "first.yml", Content: content, Project: workspace.Identity(root)}, {Path: "second.yml", Content: content, Project: workspace.Identity(root)}},
		Tools:      registry.Registrations(registry.Selections{"shellcheck": {Runner: func([]string, string) ([]byte, error) { toolCalls.Add(1); return []byte(`{"comments":[]}`), nil }}}),
		OnRulesCreated: func(rules []workflowvisit.Rule) []workflowvisit.Rule {
			if len(rules) == 0 {
				t.Error("default rules were not built")
			}
			return append(rules, &sharedCompositeObserver{Base: workflowvisit.NewBase("shared-composite-observer", ""), mu: &mu, snapshots: &snapshots, steps: &steps})
		},
	})
	if err != nil || result == nil {
		t.Fatalf("shared composite analysis failed: %v", err)
	}
	if len(snapshots) != 2 || snapshots[0] != snapshots[1] || !snapshots[0].Present() || len(steps) != 2 || steps[0] != steps[1] {
		t.Fatalf("default rules did not traverse one shared composite AST: snapshots=%p steps=%p", snapshots, steps)
	}
	if toolCalls.Load() != 2 {
		t.Fatalf("script tools did not analyze both composite invocations: %d", toolCalls.Load())
	}
	if !slices.ContainsFunc(result.Diagnostics, func(item results.PublishedDiagnostic) bool {
		return item.Rule == "action" && filepath.ToSlash(item.Path) == "local/action.yml" && strings.Contains(item.Message, `uses context "missing"`)
	}) {
		t.Fatalf("default metadata context check was bypassed: %+v", result.Diagnostics)
	}
}

func TestPublicationRechecksPreviouslyAdmittedConflictedSources(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	badPath, goodPath := filepath.Join(directory, "bad.yml"), filepath.Join(directory, "good.yml")
	content := []byte(commandGoodWorkflow)
	store, fixture := capturefixture.NewCapturedSources(t, directory)
	bad, err := fixture.Capture(badPath, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	good, err := fixture.Capture(goodPath, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	item := func(snapshot capture.CapturedDocument, message string) *finding.Finding {
		reference := source.Reference{Document: snapshot.Revision().Document(), Revision: snapshot.Revision().Digest(), Node: "document"}
		return &finding.Finding{Reference: &reference, Rule: "test", Message: message}
	}
	retained := item(good, "unrelated finding")
	result := analysisResult{files: []analyzedFile{
		{captured: bad, errors: []*finding.Finding{item(bad, "old accepted entry")}, outline: outline.WorkflowOutline{Path: badPath}},
		{captured: good, errors: []*finding.Finding{item(bad, "old accepted dependency"), retained}, outline: outline.WorkflowOutline{Path: goodPath}},
	}}
	_, err = store.Read(t.Context(), badPath, source.RawLF)
	conflict, conflicted := errors.AsType[capture.ConflictError](err)
	if !conflicted {
		t.Fatalf("model disagreement accepted: %v", err)
	}
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if err := result.revalidateCaptures(store, rows, directory, nil); !errors.Is(err, conflict) {
		t.Fatalf("publication lost source conflict: %v", err)
	}
	if result.files[0].outline != nil || len(result.files[0].errors) != 0 {
		t.Fatal("previously admitted conflicted entry remained publishable")
	}
	if len(result.files[1].errors) != 1 || result.files[1].errors[0] != retained || result.files[1].outline == nil {
		t.Fatal("publication lost unrelated observations or kept the conflicted dependency")
	}
	if string(bad.Bytes()) != string(content) {
		t.Fatal("quarantine rewrote the private captured revision")
	}
}

func TestCaptureInputsExcludeUnusedExplicitOverlays(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config, err := ConfigurationRules().ParseConfig([]byte("files: {excludes: ['**/excluded.yml']}\n"))
	if err != nil {
		t.Fatal(err)
	}
	selected, excluded := filepath.Join(root, "selected.yml"), filepath.Join(root, "excluded.yml")
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources: []sourceUnit{
			{Path: "selected.yml", InputPath: selected, Content: []byte(commandGoodWorkflow), Config: config},
			{Path: "excluded.yml", InputPath: excluded, Content: []byte("invalid: ["), Config: config},
		},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule { return nil },
	})
	if err != nil || result == nil {
		t.Fatalf("selected analysis failed: %v", err)
	}
	if !slices.Contains(result.Inputs, selected) || slices.Contains(result.Inputs, excluded) {
		t.Fatalf("overlay installation counted as a read: %v", result.Inputs)
	}
}

func TestCapturedActionInputsKeepRequestedAlias(t *testing.T) {
	t.Parallel()
	root, physical, alias := virtualActionPaths(t)
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: root,
		Sources:    []sourceUnit{{Path: "workflow.yml", Content: []byte("on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/alias/virtual}]}}\n"), Project: workspace.Identity(root)}},
		ReadFile: func(path string) ([]byte, error) {
			if filepath.Clean(path) == physical {
				return []byte(standaloneComposite), nil
			}
			return nil, os.ErrNotExist
		},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule { return nil },
	})
	if err != nil || result == nil {
		t.Fatalf("aliased action failed: %v", err)
	}
	if !slices.Contains(result.Inputs, alias) || slices.Contains(result.Inputs, physical) {
		t.Fatalf("capture replaced the consumed alias with its physical spelling: %v", result.Inputs)
	}
}

func TestPostSealOutlinesRequireConsumedManifestRequest(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                                   string
		requested, read, failed, conflict, raw bool
		want                                   int
	}{
		{name: "requested manifest", requested: true, read: true, want: 1},
		{name: "unused overlay"},
		{name: "ordinary entry read", read: true},
		{name: "failed manifest", requested: true, read: true, failed: true},
		{name: "conflicted manifest", requested: true, read: true, conflict: true},
		{name: "raw auxiliary read", requested: true, read: true, raw: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			address := filepath.Join(t.TempDir(), "action.yml")
			model := source.YAML
			if test.raw {
				model = source.RawLF
			}
			content := []byte("name: captured\ndescription: outline\nruns: {using: composite, steps: [{uses: $/must-not-read}]}\n")
			overlays := []capture.Overlay{{Path: address, Content: content, Coordinates: model}}
			if test.failed {
				overlays = nil
			}
			if test.conflict {
				overlays = append(overlays, capture.Overlay{Path: address, Content: []byte("different"), Coordinates: model})
			}
			reads := 0
			store, err := capture.New(t.Context(), overlays, func(context.Context, string) ([]byte, error) {
				reads++
				return nil, os.ErrNotExist
			})
			if err != nil {
				t.Fatal(err)
			}
			capturefixture.CloseCaptures(t, store)
			documents := &actionDocuments{byPath: make(map[string]outline.Action)}
			if test.requested {
				documents.request(address)
			}
			if test.read {
				_, err := store.Read(t.Context(), address, model)
				if (err != nil) != (test.failed || test.conflict) {
					t.Fatalf("capture outcome = %v", err)
				}
			}
			if err := store.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			consumed, err := store.ReadAliases()
			if err != nil {
				t.Fatal(err)
			}
			before := reads
			if err := documents.complete(store, consumed); err != nil {
				t.Fatal(err)
			}
			if len(documents.list()) != test.want || reads != before {
				t.Fatalf("outlines=%d, want=%d; reads before/after completion=%d/%d", len(documents.list()), test.want, before, reads)
			}
		})
	}
}

// Completion outlines a manifest composition already parsed from the
// document it parsed, not from a second parse of the sealed bytes.
func TestPostSealOutlinesReuseParsedRemoteDocument(t *testing.T) {
	t.Parallel()
	address := filepath.Join(t.TempDir(), "action.yml")
	content := []byte("name: captured\ndescription: outline\nruns: {using: composite, steps: []}\n")
	store, err := capture.New(t.Context(), []capture.Overlay{{Path: address, Content: content, Coordinates: source.YAML}}, func(context.Context, string) ([]byte, error) {
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	capturefixture.CloseCaptures(t, store)
	documents := &actionDocuments{byPath: make(map[string]outline.Action)}
	documents.request(address)
	captured, err := store.Read(t.Context(), address, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	// A stand-in for composition's parse: its outline differs from the
	// captured bytes, so a second parse would be visible.
	documents.parsed(captured, parsing.Action(address, []byte("name: parsed\ndescription: outline\nruns: {using: composite, steps: []}\n")))
	if err := store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ReadAliases()
	if err != nil {
		t.Fatal(err)
	}
	if err := documents.complete(store, consumed); err != nil {
		t.Fatal(err)
	}
	outlines := documents.list()
	if len(outlines) != 1 || outlines[0].Name != "parsed" {
		t.Fatalf("outlines = %+v, want the parsed document's outline", outlines)
	}
}
