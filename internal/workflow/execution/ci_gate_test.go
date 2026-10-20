package execution

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/yaml"
)

func TestCIActionResultGate(t *testing.T) {
	source, err := os.ReadFile("../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]yaml.Node
	}
	if err := yaml.Unmarshal(source, &workflow); err != nil {
		t.Fatal(err)
	}
	var gate struct {
		If    string
		Needs []string
		Steps []struct {
			Run string
			Env map[string]string
		}
	}
	node, exists := workflow.Jobs["action-result"]
	if !exists {
		t.Fatal("missing Action aggregate")
	}
	if err := node.Decode(&gate); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gate.Needs, []string{"action", "action-slim"}) || len(gate.Steps) != 1 {
		t.Fatalf("Action aggregate dependencies or assertion changed: %+v", gate)
	}
	expression := exprlang.ParseAssignedExpression(gate.If)
	if expression == nil {
		t.Fatalf("invalid aggregate condition: %q", gate.If)
	}
	for _, status := range []string{"success", "failure", "other"} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("condition/%s/cancelled=%t", status, cancelled), func(t *testing.T) {
				want := conditionTrue
				if cancelled {
					want = conditionFalse
				}
				if got := conditionStatusTruth(expression, status, cancelled); got != want {
					t.Fatalf("aggregate condition = %v, want %v", got, want)
				}
			})
		}
	}
	step := gate.Steps[0]
	wantRun := `if [[ "${RESULT}" == skipped && "${SLIM_RESULT}" == skipped ]]; then
  exit 0
fi
test "${RESULT}" = success && test "${SLIM_RESULT}" = success`
	if step.Env["RESULT"] != "${{ needs.action.result }}" || step.Env["SLIM_RESULT"] != "${{ needs.action-slim.result }}" || strings.TrimSpace(step.Run) != wantRun {
		t.Fatalf("aggregate must require both successful or both unselected dependencies: %+v", step)
	}
	t.Run("assertion", func(t *testing.T) {
		bash, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash is required to execute the Ubuntu aggregate assertion")
		}
		for _, result := range []string{"success", "failure", "skipped", "cancelled"} {
			for _, slim := range []string{"success", "failure", "skipped", "cancelled"} {
				t.Run(result+"/"+slim, func(t *testing.T) {
					cmd := exec.CommandContext(t.Context(), bash, "--noprofile", "--norc", "-exo", "pipefail", "-c", step.Run)
					cmd.Env = append(os.Environ(), "RESULT="+result, "SLIM_RESULT="+slim)
					output, err := cmd.CombinedOutput()
					wantSuccess := (result == "success" && slim == "success") || (result == "skipped" && slim == "skipped")
					if (err == nil) != wantSuccess {
						t.Fatalf("aggregate assertion: %v\n%s", err, output)
					}
				})
			}
		}
	})
}
