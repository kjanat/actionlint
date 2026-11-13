package managed

import (
	"context"
	"path/filepath"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	scriptmodel "actionlint.kjanat.dev/v2/internal/model/scripts"
	"actionlint.kjanat.dev/v2/internal/results"
)

type capturedScriptGuest struct{ fallbackGuest }

func (g *capturedScriptGuest) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	reference := input.Scripts[0].Reference
	return []results.Diagnostic{{Reference: &reference, Path: input.Path, Message: "captured script"}}, nil
}

func TestDocumentPluginMapsRelativeFindingToAbsoluteScriptPath(t *testing.T) {
	t.Parallel()
	batch := documentBatchFixture(t, 1)
	batch.Scripts[0].Path = filepath.Join(t.TempDir(), "workflow.yml")
	batch.Scripts[0].Content = []byte("captured content")
	guest := &capturedScriptGuest{}
	runner := documentRunner{
		documentRoute: shellcheckRoute, guest: guest, integrator: guest,
		context: tools.Context{Document: checkinputs.Input{Reference: batch.CallGraph.Entry().Reference, Path: "workflow.yml", Kind: "workflow", ParseStatus: "complete"}},
	}
	findings, err := runner.Analyze(t.Context(), batch)
	if err != nil || len(findings) != 1 {
		t.Fatalf("captured finding lost: findings=%+v error=%v", findings, err)
	}
	finding := findings[0]
	if finding.Path != batch.Scripts[0].Path || finding.Reference == nil || *finding.Reference != batch.Scripts[0].Fact.Reference || finding.Message != "captured script" || string(finding.Content) != "captured content" {
		t.Fatalf("finding has the wrong source: %+v", finding)
	}
}

func TestSourceGrantsTranslateScriptsAndInvokedRuntimeWithoutMutatingFacts(t *testing.T) {
	t.Parallel()
	scriptRuntime := &scriptmodel.Execution{Directory: "script-host", DirectoryKnown: true}
	invokedRuntime := &scriptmodel.Execution{Directory: "invoked-host", DirectoryKnown: true}
	input := checkinputs.Input{
		Scripts: []checkinputs.Script{{Runtime: scriptRuntime}, {}},
		Invoked: []checkinputs.Script{{Runtime: invokedRuntime}, {}},
	}
	files := pluginSourceFiles{mounts: []pluginSourceMount{{host: "host", guest: "/workspace"}}, directory: "/workspace"}
	files.configure(&input)
	for name, scripts := range map[string][]checkinputs.Script{"scripts": input.Scripts, "invoked": input.Invoked} {
		if scripts[0].Runtime == nil || scripts[0].Runtime.Directory != "/workspace" || !scripts[0].Runtime.DirectoryKnown || scripts[1].Runtime != nil {
			t.Errorf("%s runtime was not translated: %+v", name, scripts)
		}
	}
	if scriptRuntime.Directory != "script-host" || invokedRuntime.Directory != "invoked-host" {
		t.Fatalf("translation mutated source runtimes: script=%+v invoked=%+v", scriptRuntime, invokedRuntime)
	}
}
