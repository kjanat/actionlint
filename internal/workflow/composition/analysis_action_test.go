package composition

import (
	"path/filepath"
	"strings"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/testutil/tempdir"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func standaloneActionForTest(t *testing.T, options Options, metadata *actionmeta.Metadata, fixtures ...*capturedSnapshots) (Result, error) {
	t.Helper()
	content, available := metadata.OriginalSource()
	if !available {
		t.Fatal("action fixture has no original source")
	}
	registry := options.SourceSnapshots
	var fixture *capturedSnapshots
	if len(fixtures) != 0 {
		fixture = fixtures[0]
	}
	if registry == nil {
		fixture = newCapturedSnapshots(t, sourceFixtureBase(t))
		registry = fixture.SourceSnapshots
		options.SourceSnapshots = registry
	}
	bytes := []byte(content)
	document := documentparse.Action(metadata.Path(), metadata.Path(), bytes)
	var snapshot documentparse.SourceSnapshot
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
		address = fixture.Sources.Path(address)
	}
	deployment, err := scriptexecution.NewDeployment(registry, snapshot, address, metadata.Path(), metadata.SourceMapped)
	if err != nil {
		t.Fatal(err)
	}
	return Action(options, snapshot, deployment, metadata)
}

type mutatingStandaloneMetadata struct {
	workflowvisit.Base
}

func (*mutatingStandaloneMetadata) CheckStandaloneMetadata(metadata *actionmeta.Metadata, _ documentparse.SourceSnapshot) error {
	metadata.Runs.Using = "node24"
	metadata.Source[0] = 'x'
	return nil
}

func (rule *mutatingStandaloneMetadata) ForMetadata(*actionlocal.Resolver) workflowvisit.Rule {
	return rule
}

func TestStandaloneActionUsesCanonicalSnapshotAndDeployment(t *testing.T) {
	path := filepath.Join(tempdir.Physical(t), "action.yml")
	content := []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo canonical\n")
	document := documentparse.Action(path, path, content)
	if document.Metadata == nil || !document.CanAnalyze() {
		t.Fatal("fixture action metadata unavailable")
	}
	registry := newCapturedSnapshots(t, sourceFixtureBase(t))
	snapshot, err := registry.Document(fsread.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := scriptexecution.NewDeployment(registry.SourceSnapshots, snapshot, path, path, false)
	if err != nil {
		t.Fatal(err)
	}
	rule := &mutatingStandaloneMetadata{Base: workflowvisit.NewBase("mutate", "")}
	options := Options{
		Context: t.Context(), Actions: actionlocal.NewResolver(nil, nil), SourceSnapshots: registry.SourceSnapshots,
		CollectScripts: true, Rules: []workflowvisit.Rule{rule},
	}
	result, err := Action(options, snapshot, deployment, document.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntrySnapshot != snapshot {
		t.Fatal("action entry result replaced the canonical snapshot")
	}
	scopes, scripts := result.Batch.Snapshot()
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
	for _, invalid := range []struct {
		value string
		line  int
	}{
		{"*runs", 9}, {"[echo, bad]", 8}, {"{command: bad}", 8}, {"!unsupported bad", 9},
	} {
		t.Run(invalid.value, func(t *testing.T) {
			path := filepath.Join(tempdir.Physical(t), "action.yml")
			content := []byte("name: test\ndescription: test\nruns: &runs\n  using: composite\n  steps:\n    - shell: bash\n      run: echo before\n    - shell: bash\n      run: " + invalid.value + "\n    - shell: bash\n      run: echo after\n")
			metadata, parseErr := actionmeta.Parse(path, content)
			if metadata == nil {
				t.Fatalf("recovered metadata unavailable: %v", parseErr)
			}
			resolver := actionlocal.NewResolver(nil, nil)
			options := Options{Context: t.Context(), Actions: resolver, CollectScripts: true, Rules: []workflowvisit.Rule{builtin.NewRuleAction(resolver)}}
			result, err := standaloneActionForTest(t, options, metadata)
			if err != nil {
				t.Fatal(err)
			}
			scripts := result.Batch.Scripts()
			if len(scripts) != 2 || scripts[0].Run.Run().Value() != "echo before" || scripts[1].Run.Run().Value() != "echo after" {
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
	path := filepath.Join(tempdir.Physical(t), "action.yml")
	content := []byte("name: empty\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: ''\n")
	metadata, err := actionmeta.Parse(path, content)
	if err != nil {
		t.Fatal(err)
	}
	resolver := actionlocal.NewResolver(nil, nil)
	options := Options{Context: t.Context(), Actions: resolver, CollectScripts: true, Rules: []workflowvisit.Rule{builtin.NewRuleAction(resolver)}}
	result, err := standaloneActionForTest(t, options, metadata)
	if err != nil {
		t.Fatal(err)
	}
	scripts := result.Batch.Scripts()
	if len(result.Findings) != 0 || len(scripts) != 1 {
		t.Fatalf("empty composite script lost: scripts=%d findings=%v", len(scripts), result.Findings)
	}
	run := scripts[0].Run
	if !run.Run().SourceScalar().HasSource || !run.Source().Present() || run.Run().Value() != "" {
		t.Fatal("empty script lost its physical scalar or source mapping")
	}
}

var _ builtin.MetadataParticipant = (*mutatingStandaloneMetadata)(nil)
