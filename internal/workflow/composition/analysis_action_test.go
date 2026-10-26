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
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func standaloneActionForTest(t *testing.T, service *Service, metadata *actionmeta.Metadata) (Result, error) {
	t.Helper()
	content, available := metadata.OriginalSource()
	if !available {
		t.Fatal("action fixture has no original source")
	}
	registry := service.options.SourceSnapshots
	if registry == nil {
		registry = &documentparse.SourceSnapshots{}
		service.options.SourceSnapshots = registry
	}
	bytes := []byte(content)
	document := documentparse.Action(metadata.Path(), metadata.Path(), bytes)
	snapshot, err := registry.Document(fsread.PhysicalPath(metadata.Path()), bytes, document)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := scriptexecution.NewDeployment(snapshot, metadata.Path(), metadata.SourceMapped)
	if err != nil {
		t.Fatal(err)
	}
	return service.Action(snapshot, deployment, metadata)
}

type mutatingStandaloneMetadata struct {
	workflowvisit.Base
}

func (*mutatingStandaloneMetadata) CheckStandaloneMetadata(metadata *actionmeta.Metadata) {
	metadata.Runs.Using = "node24"
	metadata.Source[0] = 'x'
}

func (rule *mutatingStandaloneMetadata) ForMetadata(*actionlocal.Resolver) workflowvisit.Rule {
	return rule
}

func TestStandaloneActionUsesCanonicalSnapshotAndDeployment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "action.yml")
	content := []byte("name: action\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo canonical\n")
	document := documentparse.Action(path, path, content)
	if document.Metadata == nil || !document.CanAnalyze() {
		t.Fatal("fixture action metadata unavailable")
	}
	var registry documentparse.SourceSnapshots
	snapshot, err := registry.Document(fsread.PhysicalPath(path), content, document)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := scriptexecution.NewDeployment(snapshot, path, false)
	if err != nil {
		t.Fatal(err)
	}
	rule := &mutatingStandaloneMetadata{Base: workflowvisit.NewBase("mutate", "")}
	service := New(Options{
		Context: t.Context(), Actions: actionlocal.NewResolver(nil, nil), SourceSnapshots: &registry,
		CollectScripts: true, Rules: []workflowvisit.Rule{rule},
	})
	result, err := service.Action(snapshot, deployment, document.Metadata)
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

var _ builtin.MetadataParticipant = (*mutatingStandaloneMetadata)(nil)
