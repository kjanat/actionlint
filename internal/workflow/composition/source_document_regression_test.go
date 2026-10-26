package composition

import (
	"path/filepath"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
)

func TestResolvedNonCompositeActionIsAdmittedToSourceSnapshots(t *testing.T) {
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/node}, {uses: $/node}]}}\n"
	f := newInvocationFixture(t, t.Context(), workflow, map[string]string{
		"node/action.yml": "name: node\ndescription: node action\nruns: {using: node20, main: index.js}\n",
	}, nil)
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	path := fsread.PhysicalPath(filepath.Join(filepath.Dir(f.parsed[0]), "action.yml"))
	snapshot, ok := f.snapshots.LookupDocument(path)
	if !ok {
		t.Fatalf("resolved non-composite source %q was not admitted", path)
	}
	view, ok := snapshot.ActionView()
	if !ok || view.Using() != "node20" {
		t.Fatalf("canonical run kind unavailable: using=%q present=%v", view.Using(), ok)
	}
	calls := result.Batch.Invocations().Calls()
	if len(calls) != 2 || calls[0].Outcome.Kind() != invocation.OutcomeOpaque || calls[0].Outcome.Reason() != invocation.ReasonNonComposite || calls[1].Outcome.Kind() != invocation.OutcomeOpaque || calls[1].Outcome.Reason() != invocation.ReasonNonComposite {
		t.Fatalf("non-composite outcome changed: %+v", calls)
	}
	if len(f.parsed) != 1 || len(result.SourceDocuments) != 1 {
		t.Fatalf("repeated source was parsed or admitted more than once: parses=%d documents=%d", len(f.parsed), len(result.SourceDocuments))
	}
}

func TestParseFailedActionKeepsRecoveredCanonicalSourceReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "action.yml")
	content := []byte("name: recovered\ndescription: partial\ninputs: [wrong]\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo partial\n")
	document := documentparse.Action(path, path, content)
	if len(document.Findings) == 0 || document.CanAnalyze() {
		t.Fatalf("parse failure semantics changed: findings=%d canAnalyze=%v", len(document.Findings), document.CanAnalyze())
	}
	if document.Metadata == nil || len(document.Metadata.Runs.Steps) != 1 {
		t.Fatalf("parser did not recover the expected partial declaration: %#v", document.Metadata)
	}
	var snapshots documentparse.SourceSnapshots
	snapshot, err := snapshots.Document(fsread.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(snapshot.CanonicalReferences().Steps); got != 1 {
		t.Fatalf("frozen provenance lost recovered step references: got %d, want 1", got)
	}
}

func TestActionRunKindIgnoresMetadataCallbackMutation(t *testing.T) {
	workflow := "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/node}]}}\n"
	f := newInvocationFixture(t, t.Context(), workflow, map[string]string{
		"node/action.yml": "name: node\ndescription: node action\nruns: {using: node20, main: index.js}\n",
	}, nil)
	f.mutateMeta = func(metadata *actionmeta.Metadata) {
		metadata.Runs.Using = "composite"
		metadata.Source[0] = 'X'
	}
	result, err := f.service.Workflow(f.root)
	if err != nil {
		t.Fatal(err)
	}
	calls := result.Batch.Invocations().Calls()
	if len(calls) != 1 || calls[0].Outcome.Kind() != invocation.OutcomeOpaque || calls[0].Outcome.Reason() != invocation.ReasonNonComposite || calls[0].Outcome.Detail() != "node20" {
		t.Fatalf("callback mutation changed the source run kind: %+v", calls)
	}
	path := f.parsed[0]
	sourceDocument, ok := result.SourceDocuments[path]
	if !ok {
		t.Fatalf("consulted source document %q is missing", path)
	}
	const original = "name: node\ndescription: node action\nruns: {using: node20, main: index.js}\n"
	if got := string(sourceDocument.Content()); got != original {
		t.Fatalf("metadata callback mutated source content: %q", got)
	}
	result.Sources[path][0] = 'Y'
	if got := string(sourceDocument.Content()); got != original {
		t.Fatalf("consumer mutation changed source content: %q", got)
	}
	snapshot, ok := f.snapshots.LookupDocument(fsread.PhysicalPath(path))
	if !ok {
		t.Fatalf("admitted source snapshot %q is missing", path)
	}
	view, ok := snapshot.ActionView()
	if !ok || view.Using() != "node20" || string(snapshot.Content()) != original {
		t.Fatalf("mutable source values changed frozen action provenance: using=%q content=%q", view.Using(), snapshot.Content())
	}
}
