package composition

import (
	"testing"

	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
)

func prepareCollectedScripts(t *testing.T, analysis *compositeAnalyzer) scriptexecution.Batch {
	t.Helper()
	graph, err := analysis.invocations.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := scriptexecution.NewBatch(graph, scriptexecution.Collection{
		Declarations: analysis.sourceSnapshots,
		Scopes:       analysis.scriptScopes,
		Scripts:      analysis.scripts,
		Collected:    analysis.collectScripts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func requestsFromUnits(units []scriptexecution.Unit) []scriptexecution.ScriptRequest {
	requests := make([]scriptexecution.ScriptRequest, len(units))
	for i, unit := range units {
		request := scriptexecution.ScriptRequest{
			Occurrence:   unit.Occurrence,
			DefaultShell: unit.Shell,
			Execution:    unit.Execution,
		}
		if unit.Input != nil {
			site := unit.Input.Site
			request.Input = &site
		}
		requests[i] = request
	}
	return requests
}
