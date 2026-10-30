package composition

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type failingMetadataParticipant struct {
	workflowvisit.Base
	failure error
}

func (p *failingMetadataParticipant) CheckStandaloneMetadata(*actionmeta.Metadata, *documentparse.SourceSnapshot) error {
	return nil
}

func (p *failingMetadataParticipant) ForMetadata(*actionlocal.Resolver) workflowvisit.Rule {
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
