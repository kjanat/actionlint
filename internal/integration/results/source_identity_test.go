package results_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

type identityCapture struct{ input analyzer.Input }

func (*identityCapture) Descriptor() analyzer.Descriptor {
	return analyzer.Descriptor{Name: "identity-capture", Requires: []analyzer.Fact{analyzer.Source, analyzer.Scripts, analyzer.Mappings, analyzer.Dependencies}}
}

func (c *identityCapture) Analyze(_ context.Context, input analyzer.Input) ([]diagmodel.Diagnostic, error) {
	c.input = input
	return nil, nil
}

func TestFactsShareOwningStepIdentity(t *testing.T) {
	capture := &identityCapture{}
	content := []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - id: one\n        env: {ONE: x}\n        run: echo first\n      - id: two\n        env: {TWO: y}\n        run: echo second\n      - uses: owner/action@main\n        with: {value: x}\n")
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Analyzers: []analyzer.Registration{{Analyzer: capture}}}, []workspace.Source{{Path: "workflow.yml", Content: content}})
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
	result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Analyzers: []analyzer.Registration{{Analyzer: capture}}}, []workspace.Source{{Path: "workflow.yml", Content: content}})
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
			result, err := application.Analyze(t.Context(), application.Options{WorkingDir: t.TempDir(), Analyzers: []analyzer.Registration{{Analyzer: capture}}}, []workspace.Source{{Path: path, Content: content}})
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
