package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/testutil"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestCompositeMultilineScalarSuppression(t *testing.T) {
	for _, value := range []string{
		"echo first\n        ${{ github_repository }}",
		"'echo first\n        ${{ github_repository }}'",
		"\"echo first\n        ${{ github_repository }}\"",
		"!!str &script\n        \"echo first\n        ${{ github_repository }}\"",
	} {
		t.Run(value, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      # actionlint:ignore-next-line action\n      run: " + value + "\n    - shell: bash\n      run: echo ${{ github_repository }}\n"
			testutil.WriteFixture(t, root, "inner/action.yml", metadata)
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
}

func TestInlineSuppressionRunFirstKey(t *testing.T) {
	for _, value := range []string{"echo first\n        ${{ github_repository }}", "\"echo first\n        ${{ github_repository }}\""} {
		t.Run(value, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    # actionlint:ignore-next-line action\n    - run: " + value + "\n      shell: bash\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
			testutil.WriteFixture(t, root, "inner/action.yml", metadata)
			result := testutil.Composite(t, root, "- uses: ./inner", application.Options{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
	command := testutil.RequireShellcheck(t)
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\""} {
		t.Run("shellcheck/"+value, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "# actionlint:ignore-next-line shellcheck\n- run: " + value + "\n  shell: bash\n- run: echo $VALUE\n  shell: bash"
			result := testutil.Composite(t, root, steps, application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}

func TestWorkflowMultilineScalarShellcheckSuppression(t *testing.T) {
	command := testutil.RequireShellcheck(t)
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\"", "'echo first\n    $VALUE'"} {
		t.Run(value, func(t *testing.T) {
			root, _ := testutil.ExecutableFixture(t)
			testutil.WriteFixture(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "- shell: bash\n  # actionlint:ignore-next-line shellcheck\n  run: " + value + "\n- shell: bash\n  run: echo $VALUE"
			result := testutil.Composite(t, root, steps, application.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}
