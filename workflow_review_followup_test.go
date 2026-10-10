package actionlint

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestWorkflowRunPathsUntrusted(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.path",
		"github['event']['workflow_run']['path']",
		"GITHUB.EVENT.WORKFLOW_RUN.PATH",
		"format('{0}', github.event.workflow_run.path)",
	} {
		source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
		if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
			t.Fatalf("path interpolation %q: error=%v diagnostics=%+v", expression, err, result.Diagnostics)
		}
	}
	source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env: {WORKFLOW_PATH: '${{ github.event.workflow_run.path }}'}\n        run: printf '%s\\n' \"$WORKFLOW_PATH\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("path environment binding: error=%v result=%+v", err, result)
	}
}

func TestWorkflowRunNamesUntrusted(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.name",
		"github['event']['workflow_run']['name']",
		"GITHUB.EVENT.WORKFLOW_RUN.NAME",
		"format('{0}', github.event.workflow_run.name)",
	} {
		t.Run(expression, func(t *testing.T) {
			source := "on: {workflow_run: {workflows: ['Build*'], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
				t.Fatalf("name interpolation: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
			source = "on: {workflow_run: {workflows: ['Build*'], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env: {WORKFLOW_NAME: " + strconv.Quote("${{ "+expression+" }}") + "}\n        run: printf '%s\\n' \"$WORKFLOW_NAME\"\n"
			result, err = Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("name environment binding: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
		})
	}
}

func TestWorkflowRunEmptyPatternValidation(t *testing.T) {
	for _, reference := range []string{"", "${{ '' }}"} {
		t.Run(reference, func(t *testing.T) {
			source := "on: {workflow_run: {workflows: [" + strconv.Quote(reference) + "], types: [completed]}}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"
			workflow, errs := Parse([]byte(source))
			if workflow == nil || reference != "" && len(errs) != 0 {
				t.Fatalf("parse: %v", errs)
			}
			pos := *workflow.On[0].(*WebhookEvent).Workflows[0].Pos
			wantRule, wantMessage := "glob", "glob pattern cannot be empty"
			if reference == "" {
				wantRule, wantMessage = "syntax-check", "string should not be empty"
			}
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
			if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != wantRule || !strings.Contains(result.Diagnostics[0].Message, wantMessage) {
				t.Fatalf("empty pattern: error=%v diagnostics=%+v", err, result.Diagnostics)
			}
			if result.Diagnostics[0].Start.Line != pos.Line || result.Diagnostics[0].Start.Column != pos.Col {
				t.Fatalf("empty pattern position: got %+v, want %+v", result.Diagnostics[0].Start, pos)
			}
		})
	}
}

func TestWorkflowRunLiteralPatternValidation(t *testing.T) {
	for _, pattern := range []string{"Build [", "?Build", "Build*", "!Build*", "Build's CI", "${{ vars.WORKFLOW }}"} {
		reference := "${{ '" + strings.ReplaceAll(pattern, "'", "''") + "' }}"
		if strings.HasPrefix(pattern, "${{") {
			reference = pattern
		}
		workflow, errs := Parse([]byte("on: {workflow_run: {workflows: [" + strconv.Quote(reference) + "]}}\njobs: {test: {runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n"))
		if workflow == nil || len(errs) != 0 {
			t.Fatalf("parse %q: %v", reference, errs)
		}
		rule := NewRuleGlob()
		if err := rule.VisitWorkflowPre(workflow); err != nil {
			t.Fatal(err)
		}
		want := pattern == "Build [" || pattern == "?Build"
		if (len(rule.Errs()) != 0) != want {
			t.Fatalf("literal pattern %q: %v", pattern, rule.Errs())
		}
		if want {
			pos := workflow.On[0].(*WebhookEvent).Workflows[0].Pos
			if rule.Errs()[0].Line != pos.Line || rule.Errs()[0].Column != pos.Col {
				t.Fatalf("evaluated pattern finding should retain expression position: %v", rule.Errs()[0])
			}
		}
	}
}

func TestWorkflowRunDiscoveryInput(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "workflows")
	consumer := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	request := AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(dir, "consumer.yml"), Content: []byte(consumer), Project: &Project{root: root}}}}
	for _, present := range []bool{false, true} {
		if present {
			writeShellcheckFixture(t, root, ".github/workflows/build.yml", "name: Build\n"+commandGoodWorkflow)
		}
		result, err := Analyze(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(result.Inputs, dir) {
			t.Fatalf("directory discovery missing from inputs: %v", result.Inputs)
		}
		if (len(result.Diagnostics) == 0) != present {
			t.Fatalf("producer present=%v diagnostics=%+v", present, result.Diagnostics)
		}
	}
}

func TestWorkflowRunPullRequestHeadRefs(t *testing.T) {
	for _, expression := range []string{
		"github.event.workflow_run.pull_requests[0].head.ref",
		"github['event']['workflow_run']['pull_requests'][0]['head']['ref']",
		"GITHUB.EVENT.WORKFLOW_RUN.PULL_REQUESTS[0].HEAD.REF",
		"join(github.event.workflow_run.pull_requests.*.head.ref, ',')",
		"format('{0}', github.event.workflow_run.pull_requests[0].head.ref)",
	} {
		t.Run(expression, func(t *testing.T) {
			source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
			result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Rule != "expression" || !strings.Contains(result.Diagnostics[0].Message, "potentially untrusted") {
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
		source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo \"${{ " + expression + " }}\"\n"
		result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
		if err != nil || len(result.Diagnostics) != 0 {
			t.Fatalf("safe expression %q: %v, %+v", expression, err, result)
		}
	}
	source := "on: {workflow_run: {workflows: [Build], types: [completed]}}\njobs:\n  report:\n    runs-on: ubuntu-latest\n    steps:\n      - env:\n          HEAD_REF: ${{ github.event.workflow_run.pull_requests[0].head.ref }}\n        run: printf '%s\\n' \"$HEAD_REF\"\n"
	result, err := Analyze(t.Context(), AnalysisRequest{Sources: []SourceUnit{{Path: "workflow.yaml", Content: []byte(source)}}})
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("environment binding rejected: %v, %+v", err, result)
	}
}

func TestWorkflowRunLiteralReferences(t *testing.T) {
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
			root := t.TempDir()
			producer := "name: " + strconv.Quote(tc.producer) + "\n" + commandGoodWorkflow
			writeShellcheckFixture(t, root, ".github/workflows/build.yml", producer)
			consumer := "on: {workflow_run: {workflows: [" + strconv.Quote(tc.reference) + "], types: [completed]}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: root, Sources: []SourceUnit{{Path: filepath.Join(root, ".github/workflows/consumer.yml"), Content: []byte(consumer), Project: &Project{root: root}}}})
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
