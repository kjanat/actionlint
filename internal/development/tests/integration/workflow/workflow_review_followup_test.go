package workflow_test

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	workflowparser "actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/checking"
	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
	fixturefiles "actionlint.kjanat.dev/v2/internal/development/tests/support/files"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	workflowlang "actionlint.kjanat.dev/v2/internal/model/workflows/definition"
)

func TestWorkflowRunPathsUntrusted(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"github.event.workflow_run.path",
		"github['event']['workflow_run']['path']",
		"GITHUB.EVENT.WORKFLOW_RUN.PATH",
		"format('{0}', github.event.workflow_run.path)",
	} {
		source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\npermissions: {}\n"
		result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
		if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-injection" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
			t.Fatalf("path interpolation %q: error=%v diagnostics=%+v", expression, err, result.Diagnostics)
		}
	}
	source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - env: {WORKFLOW_PATH: '${{ github.event.workflow_run.path }}'}
        run: printf '%s\n' "$WORKFLOW_PATH"
permissions: {}
`
	result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("path environment binding: error=%v result=%+v", err, result)
	}
}

func TestWorkflowRunNamesUntrusted(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"github.event.workflow_run.name",
		"github['event']['workflow_run']['name']",
		"GITHUB.EVENT.WORKFLOW_RUN.NAME",
		"format('{0}', github.event.workflow_run.name)",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			source := `on: {workflow_run: {workflows: ['Build*'], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\npermissions: {}\n"
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-injection" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
				t.Fatalf("name interpolation: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
			source = `on: {workflow_run: {workflows: ['Build*'], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - env: {WORKFLOW_NAME: ` + strconv.Quote("${{ "+expression+" }}") + `}
        run: printf '%s\n' "$WORKFLOW_NAME"
permissions: {}
`
			result, err = checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("name environment binding: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
		})
	}
}

func TestWorkflowRunEmptyPatternValidation(t *testing.T) {
	t.Parallel()
	for _, reference := range []string{"", "${{ '' }}"} {
		t.Run(reference, func(t *testing.T) {
			t.Parallel()
			source := "on: {workflow_run: {workflows: [" + strconv.Quote(reference) + `], types: [completed]}}
jobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
permissions: {}
`
			workflow, errs := workflowparser.Parse([]byte(source))
			if workflow == nil || reference != "" && len(errs) != 0 {
				t.Fatalf("parse: %v", errs)
			}
			pos := *workflow.On[0].(*workflowlang.WebhookEvent).Workflows[0].Pos
			wantRule, wantMessage := "glob", "glob pattern cannot be empty"
			if reference == "" {
				wantRule, wantMessage = "syntax-check", "string should not be empty"
			}
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != wantRule || !strings.Contains(result.Diagnostics[0].Message, wantMessage) {
				t.Fatalf("empty pattern: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
			if result.Diagnostics[0].Start.Line != pos.Line || result.Diagnostics[0].Start.Column != pos.Col {
				t.Fatalf("empty pattern position: got %+v, want %+v", result.Diagnostics[0].Start, pos)
			}
		})
	}
}

func TestWorkflowRunDiscoveryInput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	consumer := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`
	request := checking.Options{WorkingDir: root}
	requestSources := []workspace.Source{{Path: filepath.Join(dir, "consumer.yml"), Content: []byte(consumer), Project: workspace.Identity(root)}}
	for _, present := range []bool{false, true} {
		if present {
			fixturefiles.Write(t, root, ".github/workflows/build.yml", "name: Build\n"+testutil.GoodWorkflow)
		}
		result, err := checking.Analyze(t.Context(), request, requestSources)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(result.Inputs, dir) != present {
			t.Fatalf("workflows directory present=%v, inputs %v", present, result.Inputs)
		}
		if (len(result.Diagnostics) == 0) != present {
			t.Fatalf("producer present=%v diagnostics=%+v", present, result.Diagnostics)
		}
	}
}

func TestWorkflowRunPullRequestHeadRefs(t *testing.T) {
	t.Parallel()
	for _, expression := range []string{
		"github.event.workflow_run.pull_requests[0].head.ref",
		"github['event']['workflow_run']['pull_requests'][0]['head']['ref']",
		"GITHUB.EVENT.WORKFLOW_RUN.PULL_REQUESTS[0].HEAD.REF",
		"join(github.event.workflow_run.pull_requests.*.head.ref, ',')",
		"format('{0}', github.event.workflow_run.pull_requests[0].head.ref)",
	} {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\npermissions: {}\n"
			result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-injection" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
				t.Fatalf("missing untrusted head-ref finding: %+v", result.Diagnostics)
			}
		})
	}
	for _, expression := range []string{
		"github.event.workflow_run.pull_requests[0].head.sha",
		"github.event.workflow_run.pull_requests[0].head.repo.id",
		"github.event.workflow_run.pull_requests[0].number",
		"contains(github.event.workflow_run.pull_requests[0].head.ref, 'release')",
	} {
		source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ ` + expression + " }}\"\npermissions: {}\n"
		result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(expression, ".sha") {
			// Payload string types do not yet prove a constrained hash alphabet.
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "script-interpolation" {
				t.Fatalf("want syntax warning for %q: %+v", expression, result.Diagnostics)
			}
		} else if len(result.Diagnostics) != 0 {
			t.Fatalf("safe expression %q: %+v", expression, result.Diagnostics)
		}
	}
	source := `on: {workflow_run: {workflows: [Build], types: [completed]}}
jobs:
  report:
    runs-on: ubuntu-latest
    steps:
      - env:
          HEAD_REF: ${{ github.event.workflow_run.pull_requests[0].head.ref }}
        run: printf '%s\n' "$HEAD_REF"
permissions: {}
`
	result, err := checking.Analyze(t.Context(), checking.Options{}, []workspace.Source{{Path: "workflow.yaml", Content: []byte(source)}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("environment binding rejected: %v, %+v", err, result)
	}
}

func TestWorkflowRunLiteralReferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		producer, reference string
		want                int
	}{
		{"Build", "${{ 'Build' }}", 0},
		{"Build", "${{ 'Typo' }}", 1},
		{"Build", "${{ 'build' }}", 1},
		{"Build", "${{ 'Build*' }}", 0},
		{"Build", "${{ 'Missing*' }}", 1},
		{"Build's CI", "${{ 'Build''s CI' }}", 0},
		{"Build", "${{ '!Missing' }}", 0},
		{"Build", "${{ vars.WORKFLOW }}", 0},
		{"Build", "${{ format('Typo') }}", 0},
		{"Build", "${{ 'Typo' }} suffix", 0},
	} {
		t.Run(tc.producer+tc.reference, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			producer := "name: " + strconv.Quote(tc.producer) + "\n" + testutil.GoodWorkflow
			fixturefiles.Write(t, root, ".github/workflows/build.yml", producer)
			consumer := "on: {workflow_run: {workflows: [" + strconv.Quote(tc.reference) + `], types: [completed]}}
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
permissions: {}
`
			result, err := checking.Analyze(t.Context(), checking.Options{WorkingDir: root}, []workspace.Source{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: workspace.Identity(root)}})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule == "workflow-run-names" {
					count++
					if diagnostic.Start.Line != 1 || tc.reference == "${{ 'Typo' }}" && !strings.Contains(diagnostic.Message, "Typo") {
						t.Fatalf("incorrect literal reference diagnostic: %+v", diagnostic)
					}
				}
			}
			if count != tc.want {
				t.Fatalf("got %d workflow-name findings, want %d: %+v", count, tc.want, result.Diagnostics)
			}
		})
	}
}
