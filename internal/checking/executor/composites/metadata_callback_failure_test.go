package composites

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	declaration "actionlint.kjanat.dev/v2/internal/model/workflows"
)

type failingMetadataParticipant struct {
	workflowvisit.Base
	failure error
}

func (p *failingMetadataParticipant) CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot, actionmeta.ReferencedFiles) error {
	return nil
}

func (p *failingMetadataParticipant) ForMetadata(*workspaceactions.Resolver) workflowvisit.Rule {
	return &failingMetadataCallback{Base: workflowvisit.NewBase(p.Name(), ""), failure: p.failure}
}

type failingMetadataCallback struct {
	workflowvisit.Base
	failure error
}

func (p *failingMetadataCallback) VisitStep(step declaration.StepView) error {
	position, _ := step.Pos()
	p.Error(&position, "partial metadata result")
	return p.failure
}

func TestMetadataCallbackFailurePreservesFindingAndSource(t *testing.T) {
	t.Parallel()
	const outer = "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n"
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": outer,
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: [{shell: bash, run: echo inner}]}\n",
	}, nil)
	failure := errors.New("metadata validation failed")
	f.options.Rules = []workflowvisit.Rule{&failingMetadataParticipant{Base: workflowvisit.NewBase("metadata-probe", ""), failure: failure}}
	result, err := Workflow(f.options, f.root)
	if !errors.Is(err, failure) || len(result.Findings) != 1 {
		t.Fatalf("partial metadata result lost: error=%v findings=%v", err, result.Findings)
	}
	snapshot := f.actionSnapshot(t, "$/outer")
	step := actionStepViews(t, snapshot)[0]
	finding := result.Findings[0]
	if finding.Message != "partial metadata result" || finding.Path != filepath.Join(f.options.Paths.Workspace, "outer", "action.yml") || !bytes.Equal(finding.Source, []byte(outer)) || finding.Reference == nil || *finding.Reference != step.Reference() {
		t.Fatalf("partial metadata result has incorrect provenance: %+v", finding)
	}
}

func TestMetadataCallbacksRunForEveryParticipant(t *testing.T) {
	t.Parallel()
	f := newInvocationFixture(t, t.Context(), "on: push\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{
		"outer/action.yml": "name: outer\ndescription: outer\nruns: {using: composite, steps: [{uses: $/inner}]}\n",
		"inner/action.yml": "name: inner\ndescription: inner\nruns: {using: composite, steps: []}\n",
	}, nil)
	f.options.Rules = []workflowvisit.Rule{
		&failingMetadataParticipant{Base: workflowvisit.NewBase("first-metadata-check", "")},
		&failingMetadataParticipant{Base: workflowvisit.NewBase("second-metadata-check", "")},
	}
	result, err := Workflow(f.options, f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 2 || result.Findings[0].Rule != "first-metadata-check" || result.Findings[1].Rule != "second-metadata-check" {
		t.Fatalf("metadata checks did not all run: %+v", result.Findings)
	}
	first, second := result.Findings[0].Reference, result.Findings[1].Reference
	if first == nil || second == nil || *first != *second {
		t.Fatal("metadata checks reported different source declarations")
	}
}
