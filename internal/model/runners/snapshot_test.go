package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

func TestSnapshotInventoryValidation(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		`null`, `{}`, `{"runners":null}`, `{"runners":[null]}`, `{"runners":[],"typo":true}`,
		`{"runners":[{"name":"a","shell":"pwsh"}]}`,
		`{"runners":[{"name":"a","os":"win"}]}`,
		`{"runners":[{"name":"a","case_sensitivity":"ignore"}]}`,
		`{"runners":[{"name":"a","labels":[""]}]}`,
		`{"runners":[{"name":"a"},{"name":"a"}]}`,
	} {
		_, err := (snapshotProvider{}).ResolveRunners(t.Context(), runnerapi.Request{
			Target: "github", Snapshot: runnerapi.Snapshot{Content: content},
		})
		if err == nil {
			t.Errorf("accepted invalid inventory: %s", content)
		}
	}
}
