package checking

import (
	"context"
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rulemeta "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/results"
)

type sourceScopedChecker struct{}

func (sourceScopedChecker) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "source-check", Category: "correctness", Maturity: "stable", Recommended: true, Scope: rulemeta.SourceConfiguration, SupportsActions: true, RequiredInputs: []rulemeta.InputKind{rulemeta.Scripts, rulemeta.Invocations}}
}

func (sourceScopedChecker) CheckDocument(_ context.Context, input checkinputs.Input) ([]results.Diagnostic, error) {
	var findings []results.Diagnostic
	for _, scripts := range [][]checkinputs.Script{input.Scripts, input.Invoked} {
		for _, script := range scripts {
			findings = append(findings, results.Diagnostic{Reference: &script.Reference, Path: script.Path, Rule: "source-check", Message: "source-scoped finding", Start: script.Start})
		}
	}
	return findings, nil
}

func TestRegisteredSourceScopeUsesActionConfiguration(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSuppressionFixture(t, root, ".git/HEAD", "ref: refs/heads/master\n")
	writeSuppressionFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\nlint:\n  rules:\n    correctness: {source-check: error}\noverrides:\n  - includes: ['actions/**']\n    lint:\n      rules:\n        correctness: {source-check: info}\n")
	action := writeSuppressionFixture(t, root, "actions/local/action.yml", "name: Local\ndescription: Test\nruns:\n  using: composite\n  steps:\n    - run: echo hello\n      shell: bash\n")
	workflow := writeSuppressionFixture(t, root, ".github/workflows/ci.yml", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./actions/local\n")
	for _, path := range []string{action, workflow} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			result, err := Check(t.Context(), Options{WorkingDir: root, Analyzers: []checkinputs.Registration{{Checker: sourceScopedChecker{}}}}, inputmodel.Request{Kind: inputmodel.Files, Paths: []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			var count int
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "source-check" {
					continue
				}
				count++
				if diagnostic.Severity != "info" {
					t.Fatalf("action source override was lost: %+v", diagnostic)
				}
			}
			if count != 1 {
				t.Fatalf("got %d plugin findings, want one: %+v", count, result.Diagnostics)
			}
		})
	}
}
