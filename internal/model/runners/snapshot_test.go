package runner

import (
	"testing"

	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

const desktopInventory = `{"runners":[{"name":"desktop","labels":["self-hosted","desktop-pool","x64"],"group":"build","os":"windows","default_shell":"pwsh","case_sensitivity":"insensitive"},{"name":"server","labels":["self-hosted","server-pool","arm64"],"group":"build","os":"linux","default_shell":"bash"}]}`

func TestSnapshotInventoryValidation(t *testing.T) {
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
