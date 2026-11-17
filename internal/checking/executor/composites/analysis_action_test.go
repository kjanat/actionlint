package composites

import (
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness"
	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/capturefixture/declarations"

	"actionlint.kjanat.dev/v2/internal/checking/workflowvisit"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"

	"actionlint.kjanat.dev/v2/internal/development/tests/testutil/tempdir"
	"actionlint.kjanat.dev/v2/internal/input/files"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"

	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
)

func standaloneActionForTest(t *testing.T, options Options, metadata *actionmeta.Metadata, fixtures ...*declarations.CapturedSnapshots) (Composition, error) {
	t.Helper()
	content, available := metadata.OriginalSource()
	if !available {
		t.Fatal("action fixture has no original source")
	}
	registry := options.SourceSnapshots
	var fixture *declarations.CapturedSnapshots
	if len(fixtures) != 0 {
		fixture = fixtures[0]
	}
	if registry == nil {
		registry, _, fixture = newCapturedSnapshots(t, sourceFixtureBase(t))
		options.SourceSnapshots = registry
	}
	bytes := []byte(content)
	document := parsing.Action(metadata.Path(), bytes)
	var snapshot parsing.SourceSnapshot
	var err error
	if fixture != nil {
		snapshot, err = fixture.Document(metadata.Path(), bytes, document)
	} else {
		captured, captureErr := registry.Capture(metadata.Path())
		if captureErr != nil {
			t.Fatal(captureErr)
		}
		snapshot, err = registry.Document(captured, document)
	}
	if err != nil {
		t.Fatal(err)
	}
	address := metadata.Path()
	if fixture != nil {
		address = fixture.Path(address)
	}
	deployment, err := scriptanalysis.NewDeployment(registry, snapshot, address, metadata.Path(), metadata.SourceMapped)
	if err != nil {
		t.Fatal(err)
	}
	return Action(options, snapshot, deployment, metadata)
}

type mutatingStandaloneMetadata struct {
	workflowvisit.Base
}

func (*mutatingStandaloneMetadata) CheckStandaloneMetadata(metadata *actionmeta.Metadata, _ parsing.SourceSnapshot, _ actionmeta.ReferencedFiles) error {
	metadata.Runs.Using = "node24"
	metadata.Source[0] = 'x'
	return nil
}

func (rule *mutatingStandaloneMetadata) ForMetadata(*localactions.Resolver) workflowvisit.MetadataParticipant {
	return rule
}

func TestStandaloneActionUsesCanonicalSnapshotAndDeployment(t *testing.T) {
	t.Parallel()
	path := filepath.Join(tempdir.Physical(t), "action.yml")
	content := []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo canonical\n")
	document := parsing.Action(path, content)
	if document.Metadata == nil || !document.CanAnalyze() {
		t.Fatal("fixture action metadata unavailable")
	}
	registry, _, fixture := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := fixture.Document(files.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := scriptanalysis.NewDeployment(registry, snapshot, path, path, false)
	if err != nil {
		t.Fatal(err)
	}
	rule := &mutatingStandaloneMetadata{Base: workflowvisit.NewBase("mutate", "")}
	options := Options{
		Bindings: testBindings(nil),
		Context:  t.Context(), Actions: localactions.NewResolver(nil, nil), SourceSnapshots: registry,
		CollectScripts: true, Rules: []workflowvisit.Rule{rule},
	}
	result, err := Action(options, snapshot, deployment, document.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntrySnapshot != snapshot {
		t.Fatal("action entry result replaced the canonical snapshot")
	}
	scopes, scripts := result.Batch.Scopes, result.Batch.Scripts
	if len(scopes) != 1 || !scopes[0].Context.IsEntry() || !scopes[0].Composite || len(scripts) != 1 {
		t.Fatalf("mutable metadata changed action classification or expansion: scopes=%+v scripts=%+v", scopes, scripts)
	}
	if scopes[0].Deployment.SourcePath() != path || scopes[0].Deployment.ActionPath() != filepath.Dir(path) {
		t.Fatalf("standalone action deployment changed: %+v", scopes[0].Deployment)
	}
	if !strings.EqualFold(document.Metadata.Runs.Using, "node24") {
		t.Fatal("mutation callback did not run")
	}
}

func TestStandaloneCompositionSkipsRejectedRunDeclarations(t *testing.T) {
	t.Parallel()
	for _, invalid := range []struct {
		value string
		line  int
	}{
		{"*runs", 9}, {"[echo, bad]", 8}, {"{command: bad}", 8}, {"!unsupported bad", 9},
	} {
		t.Run(invalid.value, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(tempdir.Physical(t), "action.yml")
			content := []byte("name: test\ndescription: test\nruns: &runs\n  using: composite\n  steps:\n    - shell: bash\n      run: echo before\n    - shell: bash\n      run: " + invalid.value + "\n    - shell: bash\n      run: echo after\n")
			metadata, parseErr := actionmeta.ParseActionMetadata(path, content)
			if metadata == nil {
				t.Fatalf("recovered metadata unavailable: %v", parseErr)
			}
			resolver := localactions.NewResolver(nil, nil)
			options := Options{Bindings: testBindings(nil), Context: t.Context(), Actions: resolver, CollectScripts: true, Rules: []workflowvisit.Rule{correctness.NewRuleAction(t.Context(), correctness.ActionRule.Base(), resolver, nil)}}
			result, err := standaloneActionForTest(t, options, metadata)
			if err != nil {
				t.Fatal(err)
			}
			scripts := result.Batch.Scripts
			if len(scripts) != 2 || scripts[0].Run.Run.Text() != "echo before" || scripts[1].Run.Run.Text() != "echo after" {
				t.Fatalf("rejected run entered standalone script batch: %+v", scripts)
			}
			found := false
			for _, finding := range result.Findings {
				if finding.Rule == "action" && finding.Start.Line == invalid.line && finding.Path == path {
					found = true
				}
			}
			if !found {
				t.Fatalf("rejected run diagnostic lost through composition: %+v", result.Findings)
			}
		})
	}
}

func TestStandaloneEmptyRunKeepsScriptOccurrence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(tempdir.Physical(t), "action.yml")
	content := []byte("name: empty\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ''\n")
	metadata, err := actionmeta.ParseActionMetadata(path, content)
	if err != nil {
		t.Fatal(err)
	}
	resolver := localactions.NewResolver(nil, nil)
	options := Options{Bindings: testBindings(nil), Context: t.Context(), Actions: resolver, CollectScripts: true, Rules: []workflowvisit.Rule{correctness.NewRuleAction(t.Context(), correctness.ActionRule.Base(), resolver, nil)}}
	result, err := standaloneActionForTest(t, options, metadata)
	if err != nil {
		t.Fatal(err)
	}
	scripts := result.Batch.Scripts
	if len(result.Findings) != 0 || len(scripts) != 1 {
		t.Fatalf("empty composite script lost: scripts=%d findings=%v", len(scripts), result.Findings)
	}
	run := scripts[0].Run
	if !run.Run.SourceScalar.HasSource || run.Source == nil || run.Run.Text() != "" {
		t.Fatal("empty script lost its physical scalar or source mapping")
	}
}

var _ workflowvisit.MetadataParticipant = (*mutatingStandaloneMetadata)(nil)
