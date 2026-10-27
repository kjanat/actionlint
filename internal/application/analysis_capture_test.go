package application

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	documentmodel "actionlint.kjanat.dev/v2/document"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/source/capture"
	"actionlint.kjanat.dev/v2/internal/testutil/capturefixture"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
	"actionlint.kjanat.dev/v2/source"
)

func TestSourceCaptureJoinCancelsAndRetriesBeforePublication(t *testing.T) {
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
}

func TestSourceCapturePersistentJoinFailureBlocksPublication(t *testing.T) {
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
	workflows *[]*workflowlang.Workflow
}

func (r *capturedTraversal) VisitWorkflowPre(workflow *workflowlang.Workflow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.workflows = append(*r.workflows, workflow)
	return nil
}

func TestDuplicateEntriesTraverseOneCanonicalCapturedAST(t *testing.T) {
	var mu sync.Mutex
	var workflows []*workflowlang.Workflow
	content := []byte(commandGoodWorkflow)
	result, err := runTestAnalysis(t.Context(), analysisRequest{
		WorkingDir: t.TempDir(),
		Sources:    []sourceUnit{{Path: "workflow.yml", Content: content}, {Path: "workflow.yml", Content: content}},
		OnRulesCreated: func([]workflowvisit.Rule) []workflowvisit.Rule {
			return []workflowvisit.Rule{&capturedTraversal{Base: workflowvisit.NewBase("capture-traversal", ""), mu: &mu, workflows: &workflows}}
		},
	})
	if err != nil || result == nil {
		t.Fatalf("duplicate entry analysis failed: %v", err)
	}
	if len(workflows) != 2 || workflows[0] != workflows[1] {
		t.Fatalf("entries traversed independently decoded ASTs: %p", workflows)
	}
}

func TestPublicationRechecksPreviouslyAdmittedConflictedSources(t *testing.T) {
	directory := t.TempDir()
	badPath, goodPath := filepath.Join(directory, "bad.yml"), filepath.Join(directory, "good.yml")
	content := []byte(commandGoodWorkflow)
	fixture := capturefixture.NewCapturedSources(t, directory)
	bad, err := fixture.Capture(badPath, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	good, err := fixture.Capture(goodPath, content, source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	finding := func(snapshot capture.Snapshot, message string) *findingmodel.Finding {
		reference := source.Reference{Document: snapshot.Revision().Document(), Revision: snapshot.Revision().Digest(), Node: "document"}
		return &findingmodel.Finding{Reference: &reference, Rule: "test", Message: message}
	}
	retained := finding(good, "unrelated finding")
	result := analysisResult{files: []analyzedFile{
		{captured: bad, errors: []*findingmodel.Finding{finding(bad, "old accepted entry")}, outline: documentmodel.Workflow{Path: badPath}},
		{captured: good, errors: []*findingmodel.Finding{finding(bad, "old accepted dependency"), retained}, outline: documentmodel.Workflow{Path: goodPath}},
	}}
	_, err = fixture.Store.Read(t.Context(), badPath, source.RawLF)
	conflict, conflicted := errors.AsType[*capture.ConflictError](err)
	if !conflicted {
		t.Fatalf("model disagreement accepted: %v", err)
	}
	if err := fixture.Store.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.Store.Sources()
	if err != nil {
		t.Fatal(err)
	}
	if err := result.revalidateCaptures(fixture.Store, rows, nil); !errors.Is(err, conflict) {
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
