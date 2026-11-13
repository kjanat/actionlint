package process_test

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/process"
)

func TestSARIFComponentSeverityResolution(t *testing.T) {
	t.Parallel()
	const log = `{"version":"2.1.0","runs":[{"tool":{"driver":{"guid":"driver-guid","rules":[{"id":"shared","defaultConfiguration":{"level":"warning"}}]},"extensions":[{"guid":"extension-guid","rules":[{"id":"shared","guid":"rule-guid","defaultConfiguration":{"level":"note"}}]}]},"invocations":[{"ruleConfigurationOverrides":[{"descriptor":{"guid":"rule-guid","toolComponent":{"guid":"extension-guid"}},"configuration":{"level":"error"}}]}],"results":[{"rule":{"index":0,"toolComponent":{"index":0,"guid":"extension-guid"}},"provenance":{"invocationIndex":0},"message":{"text":"finding"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"workflow.yml"},"region":{"startLine":1}}}]},{"rule":{"guid":"rule-guid","toolComponent":{"guid":"extension-guid"}},"message":{"text":"finding"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"workflow.yml"},"region":{"startLine":1}}}]},{"rule":{"index":0,"toolComponent":{"guid":"driver-guid"}},"message":{"text":"finding"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"workflow.yml"},"region":{"startLine":1}}}]}]}]}`
	got, err := (process.SARIF{}).Decode(context.Background(), []byte(log), map[string]string{"filename": "workflow.yml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Diagnostics) != 3 {
		t.Fatalf("got %d findings", len(got.Diagnostics))
	}
	for i, want := range []string{"error", "note", "warning"} {
		if got.Diagnostics[i].Code != "shared" || got.Diagnostics[i].Severity != want {
			t.Errorf("finding %d: %+v", i, got.Diagnostics[i])
		}
	}
}
