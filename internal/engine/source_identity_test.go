package engine

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analyzer"
	"actionlint.kjanat.dev/diagnostic"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/source"
)

type identityCapture struct{ input analyzer.Input }

func (*identityCapture) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "identity-capture", Requires: []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Mappings, analyzer.Dependencies}}
}
func (c *identityCapture) Analyze(_ context.Context, input analyzer.Input) ([]diagnostic.Diagnostic, error) {
	c.input = input
	return nil, nil
}

func TestFactsShareOwningStepIdentity(t *testing.T) {
	capture := &identityCapture{}
	content := []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: one\n        env: {ONE: x}\n        run: echo first\n      - id: two\n        env: {TWO: y}\n        run: echo second\n      - uses: owner/action@main\n        with: {value: x}\n")
	result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: content}}, Analyzers: []analyzer.Registration{{Analyzer: capture}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	input := capture.input
	if len(input.Scripts) != 2 || len(input.Mappings) != 3 || len(input.Dependencies) != 1 {
		t.Fatalf("facts: %+v", input)
	}
	for i, script := range input.Scripts {
		if script.Reference.Node == "" || script.Reference.Node != input.Mappings[i].Reference.Node || script.Reference.Document != input.Reference.Document {
			t.Fatalf("owning identity lost: script=%+v mapping=%+v", script.Reference, input.Mappings[i].Reference)
		}
	}
	if input.Scripts[0].Reference.Node == input.Scripts[1].Reference.Node || input.Mappings[0].Scope == input.Mappings[1].Scope {
		t.Fatal("sibling identity collision")
	}
	if !strings.Contains(input.Mappings[1].Scope, "steps[1].env") {
		t.Fatal(input.Mappings[1].Scope)
	}
	if input.Dependencies[0].Reference.Node != input.Mappings[2].Reference.Node || input.Dependencies[0].End.Column <= input.Dependencies[0].Start.Column {
		t.Fatal(input.Dependencies)
	}
}

func TestDiagnosticOwnersWithoutIgnoreConfiguration(t *testing.T) {
	capture := &identityCapture{}
	content := []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: one\n        env: {'BAD NAME': x}\n        run: echo first\n      - id: two\n        env: {'OTHER BAD': y}\n        run: echo second\n")
	result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: "workflow.yml", Content: content}}, Analyzers: []analyzer.Registration{{Analyzer: capture}}})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range result.Diagnostics {
		if d.Rule != "env-var" {
			continue
		}
		if d.Reference == nil || *d.Reference != capture.input.Mappings[count].Reference {
			t.Fatalf("diagnostic lost owner: %+v", d)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected two findings, got %d", count)
	}
}

func TestAliasedScriptsRetainOwningStepIdentity(t *testing.T) {
	for _, kind := range []string{"workflow", "action"} {
		t.Run(kind, func(t *testing.T) {
			prefix, path, indent := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n", "workflow.yml", "      "
			if kind == "action" {
				prefix, path, indent = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n", "action.yml", "    "
			}
			steps := "- id: one\n  env: {ONE: x}\n  shell: bash\n  run: &command echo hello\n- id: two\n  env: {TWO: y}\n  shell: bash\n  run: *command\n"
			content := []byte(prefix + indent + strings.ReplaceAll(strings.TrimSuffix(steps, "\n"), "\n", "\n"+indent) + "\n")
			capture := &identityCapture{}
			result, err := Analyze(t.Context(), AnalysisRequest{WorkingDir: t.TempDir(), Sources: []SourceUnit{{Path: path, Content: content}}, Analyzers: []analyzer.Registration{{Analyzer: capture}}})
			if err != nil || len(result.Diagnostics) != 0 {
				t.Fatalf("analyze: %v, %+v", err, result)
			}
			input := capture.input
			if len(input.Scripts) != 2 || len(input.Mappings) != 2 {
				t.Fatalf("facts: %+v", input)
			}
			for i, script := range input.Scripts {
				if script.Reference.Node == "" || script.Reference != input.Mappings[i].Reference {
					t.Fatalf("owner mismatch: script=%+v mapping=%+v", script.Reference, input.Mappings[i].Reference)
				}
			}
			if input.Scripts[0].Reference.Node == input.Scripts[1].Reference.Node {
				t.Fatal("aliases merged distinct step identities")
			}
		})
	}
}

func TestAnonymousStepIdentitySurvivesReordering(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"env", "run: echo hello\n  env: {FIRST: value}", "run: echo hello\n  env: {SECOND: value}"},
		{"inputs", "uses: owner/action@main\n  with: {key: first}", "uses: owner/action@main\n  with: {key: second}"},
		{"condition", "run: echo hello\n  if: success()", "run: echo hello\n  if: failure()"},
		{"directory", "run: echo hello\n  working-directory: first", "run: echo hello\n  working-directory: second"},
		{"timeout", "run: echo hello\n  timeout-minutes: 1", "run: echo hello\n  timeout-minutes: 2"},
		{"continue", "run: echo hello\n  continue-on-error: true", "run: echo hello\n  continue-on-error: false"},
		{"background", "run: echo hello\n  background: true", "run: echo hello\n  background: false"},
		{"insertions", "run: echo hello\n  env:\n    '${{ insert }}': {KEY: first}", "run: echo hello\n  env:\n    '${{ insert }}': {KEY: second}"},
		{"parallel", "parallel:\n    - run: echo first", "parallel:\n    - run: echo second"},
		{"wait", "wait: first", "wait: second"},
		{"cancel", "cancel: first", "cancel: second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := stepReferencesForTest(t, tc.first, tc.second)
			after := stepReferencesForTest(t, tc.second, tc.first)
			if before[0].Node == before[1].Node || before[0].Node != after[1].Node || before[1].Node != after[0].Node {
				t.Fatalf("identities followed positions: before=%+v after=%+v", before, after)
			}
			if before[0].Address == after[1].Address {
				t.Fatal("structural address did not follow the move")
			}
			if tc.name == "parallel" && (before[2].Node != after[3].Node || before[3].Node != after[2].Node) {
				t.Fatal("parallel children lost their owning identity")
			}
		})
	}
}

func TestStepIdentityIgnoresPresentation(t *testing.T) {
	before := stepReferencesForTest(t, "run: echo hello\n  env: {FIRST: one, SECOND: two}")
	after := stepReferencesForTest(t, "# comment\n  run: 'echo hello'\n  env: {SECOND: two, FIRST: one}")
	if before[0].Node != after[0].Node {
		t.Fatal("comment, quoting, or mapping order changed identity")
	}
}

func stepReferencesForTest(t *testing.T, steps ...string) []source.Reference {
	t.Helper()
	var text strings.Builder
	text.WriteString("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n")
	for _, step := range steps {
		text.WriteString("      - ")
		text.WriteString(strings.ReplaceAll(step, "\n", "\n      "))
		text.WriteString("\n")
	}
	content := text.String()
	parsed := parseDocument("workflow.yml", "workflow.yml", []byte(content))
	if len(parsed.findings) != 0 {
		t.Fatal(parsed.findings)
	}
	refs := workflowReferences("workflow.yml", []byte(content), parsed.workflow, false)
	var result []source.Reference
	for _, step := range parsed.workflow.Jobs["build"].Steps {
		result = append(result, refs.Steps[step])
	}
	for _, step := range parsed.workflow.Jobs["build"].Steps {
		if parallel, ok := step.Exec.(*workflowlang.ExecParallel); ok {
			for _, child := range parallel.Steps {
				result = append(result, refs.Steps[child])
			}
		}
	}
	return result
}
