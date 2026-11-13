package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
)

func TestCompositeMultilineScalarSuppression(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"echo first\n        ${{ github_repository }}",
		"'echo first\n        ${{ github_repository }}'",
		"\"echo first\n        ${{ github_repository }}\"",
		"!!str &script\n        \"echo first\n        ${{ github_repository }}\"",
	} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      # actionlint:ignore-next-line action\n      run: " + value + "\n    - shell: bash\n      run: echo ${{ github_repository }}\n"
			fixturefiles.Write(t, root, "inner/action.yml", metadata)
			result := testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
}

func TestInlineSuppressionRunFirstKey(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"echo first\n        ${{ github_repository }}", "\"echo first\n        ${{ github_repository }}\""} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    # actionlint:ignore-next-line action\n    - run: " + value + "\n      shell: bash\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
			fixturefiles.Write(t, root, "inner/action.yml", metadata)
			result := testutil.Composite(t, root, "- uses: ./inner", checking.Options{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
	command := testutil.RequireTool(t, "shellcheck")
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\""} {
		t.Run("shellcheck/"+value, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "# actionlint:ignore-next-line shellcheck\n- run: " + value + "\n  shell: bash\n- run: echo $VALUE\n  shell: bash"
			result := testutil.Composite(t, root, steps, checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}

func TestWorkflowMultilineScalarShellcheckSuppression(t *testing.T) {
	t.Parallel()
	command := testutil.RequireTool(t, "shellcheck")
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\"", "'echo first\n    $VALUE'"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			root, _ := testutil.ExecutableFixture(t, "shellcheck")
			fixturefiles.Write(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "- shell: bash\n  # actionlint:ignore-next-line shellcheck\n  run: " + value + "\n- shell: bash\n  run: echo $VALUE"
			result := testutil.Composite(t, root, steps, checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: command}})})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}
