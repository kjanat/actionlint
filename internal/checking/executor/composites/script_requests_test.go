package composites

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/scripts"
)

func prepareCollectedScripts(t *testing.T, analysis *compositeAnalyzer) scripts.Batch {
	t.Helper()
	graph, err := analysis.invocations.Publish()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := scripts.NewBatch(graph, scripts.Collection{
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

func requestsFromUnits(units []scripts.Unit) []scripts.ScriptRequest {
	requests := make([]scripts.ScriptRequest, len(units))
	for i, unit := range units {
		request := scripts.ScriptRequest{
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
