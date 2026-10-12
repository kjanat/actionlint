package actionlint

import (
	"strings"
	"testing"
)

func TestInlineSuppressionFlowScalarBoundary(t *testing.T) {
	for _, value := range []string{"\"first\n    body_marker\"", "'first\n    body_marker'", "first\n    body_marker"} {
		t.Run(value, func(t *testing.T) {
			source := "# actionlint:ignore-next-line expression\n- {run: " + value + ", shell: bash}\n- run: sibling\n"
			body := &Error{Line: 3, Kind: "expression"}
			sibling := &Error{Line: 4, Kind: "expression"}
			got := filterInlineSuppressions([]byte(source), []*Error{body, sibling}, nil)
			if len(got) != 1 || got[0] != sibling {
				t.Fatalf("flow scalar suppression must end before sibling: %+v", got)
			}
		})
	}
}

func TestInlineSuppressionMultilineScalarBoundary(t *testing.T) {
	for _, value := range []string{
		"first\n  body_marker",
		"'first\n  body_marker'",
		"\"first\n  body_marker\"",
		"\"first \\\"quoted\\\"\n  body_marker\"",
		"\"first \\\n  body_marker\n  \"",
		"'first ''quoted''\n  body_marker\n  '",
		"!!str &script\n  first\n  body_marker",
		"&script !!str\n  'first\n  body_marker'",
		"!!str\n  &script\n  \"first\n  body_marker\"",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(value+ending, func(t *testing.T) {
				input := "# actionlint:ignore-next-line expression\nrun: " + value + "\nnext: sibling\n"
				body, sibling := 0, 0
				for i, line := range strings.Split(input, "\n") {
					if strings.Contains(line, "body_marker") {
						body = i + 1
					}
					if strings.Contains(line, "next:") {
						sibling = i + 1
					}
				}
				input = strings.ReplaceAll(input, "\n", ending)
				findings := []*Error{{Line: body, Kind: "expression"}, {Line: sibling, Kind: "expression"}}
				got := filterInlineSuppressions([]byte(input), findings, nil)
				if len(got) != 1 || got[0] != findings[1] {
					t.Fatalf("scalar body should be ignored without swallowing sibling: %+v", got)
				}
				comments := collectInlineSuppressionComments([]byte(input))
				if len(comments) != 1 || comments[0].startLine != 2 || comments[0].endLine != sibling-1 {
					t.Fatalf("wrong complete scalar extent: %+v", comments)
				}
			})
		}
	}
}

func TestCompositeMultilineScalarSuppression(t *testing.T) {
	for _, value := range []string{
		"echo first\n        ${{ github_repository }}",
		"'echo first\n        ${{ github_repository }}'",
		"\"echo first\n        ${{ github_repository }}\"",
		"!!str &script\n        \"echo first\n        ${{ github_repository }}\"",
	} {
		t.Run(value, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      # actionlint:ignore-next-line action\n      run: " + value + "\n    - shell: bash\n      run: echo ${{ github_repository }}\n"
			writeShellcheckFixture(t, root, "inner/action.yml", metadata)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
}

func TestInlineSuppressionRunFirstKey(t *testing.T) {
	for _, value := range []string{"echo first\n        ${{ github_repository }}", "\"echo first\n        ${{ github_repository }}\""} {
		t.Run(value, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			metadata := "name: inner\ndescription: test\nruns:\n  using: composite\n  steps:\n    # actionlint:ignore-next-line action\n    - run: " + value + "\n      shell: bash\n    - run: echo ${{ github_repository }}\n      shell: bash\n"
			writeShellcheckFixture(t, root, "inner/action.yml", metadata)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "action" || !strings.Contains(result.Diagnostics[0].Message, "runs.steps[1]") {
				t.Fatalf("expected only unsuppressed sibling action: %+v", result.Diagnostics)
			}
		})
	}
	command := shellcheckForTest(t)
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\""} {
		t.Run("shellcheck/"+value, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "# actionlint:ignore-next-line shellcheck\n- run: " + value + "\n  shell: bash\n- run: echo $VALUE\n  shell: bash"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}

func TestWorkflowMultilineScalarShellcheckSuppression(t *testing.T) {
	command := shellcheckForTest(t)
	for _, value := range []string{"echo first\n    $VALUE", "\"echo first\n    $VALUE\"", "'echo first\n    $VALUE'"} {
		t.Run(value, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {ruff: false}\n")
			steps := "- shell: bash\n  # actionlint:ignore-next-line shellcheck\n  run: " + value + "\n- shell: bash\n  run: echo $VALUE"
			result := compositeAnalysis(t, root, steps, AnalysisOptions{Shellcheck: command})
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "shellcheck" || !strings.Contains(result.Diagnostics[0].Snippet, "run: echo $VALUE") {
				t.Fatalf("expected only unsuppressed sibling script: %+v", result.Diagnostics)
			}
		})
	}
}
