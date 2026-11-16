package composites

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
)

func prepareCollectedScripts(t *testing.T, analysis *compositeAnalyzer) scriptanalysis.Batch {
	t.Helper()
	graph, err := analysis.invocations.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := scriptanalysis.NewBatch(graph, scriptanalysis.Collection{
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

func requestsFromUnits(units []scriptanalysis.Unit) []scriptanalysis.ScriptRequest {
	requests := make([]scriptanalysis.ScriptRequest, len(units))
	for i, unit := range units {
		request := scriptanalysis.ScriptRequest{
			Occurrence:   unit.Occurrence,
			DefaultShell: unit.Shell,
			Execution:    unit.Execution,
			Environment:  unit.Environment,
		}
		if unit.Input != nil {
			site := unit.Input.Site
			request.Input = &site
		}
		requests[i] = request
	}
	return requests
}
