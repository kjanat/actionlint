package checking_test

import (
	"context"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/model/commands"
	"actionlint.kjanat.dev/v2/internal/results"
)

type interpreterProvider struct{}

func (interpreterProvider) Descriptor() checkinputs.Descriptor {
	return checkinputs.Descriptor{Name: "wrapper-options", Category: "correctness", Maturity: "stable", RequiredInputs: []rule.InputKind{rule.Source}, SupportsActions: true, InvocationDefinitions: []commands.Definition{
		{ID: "example/runtime", Language: "typescript", Options: []commands.Option{{Long: "unsafe", Warning: "Sandbox disabled."}}},
		{ID: "example/wrapper", Executables: []string{"actions-shell"}, Prefix: []string{"bun"}, Delegate: "example/runtime"},
	}}
}

func (interpreterProvider) CheckDocument(context.Context, checkinputs.Input) ([]results.Diagnostic, error) {
	return nil, nil
}

func TestShellInvocationValidationThroughSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ shell, code, severity string }{
		{"bash -euo -pipefail {0}", "invalid-option-value", "error"},
		{"bash -euo pipefail {0}", "", ""},
		{"bash -O definitely_not_a_shopt {0}", "invalid-option-value", "error"},
		{"bash -O extglob {0}", "", ""},
		{"actions-shell bun --unsafe {0}", "interpreter-option", "warning"},
		{"actions-shell bun {0}", "", ""},
		{"actions-shell rust {0}", "", ""},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			t.Parallel()
			source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: " + tc.shell + "\n        run: echo hi\n"
			result, err := checking.Check(t.Context(), checking.Options{SkipProjectConfig: true, Analyzers: []checkinputs.Registration{{Checker: interpreterProvider{}}}}, inputmodel.Request{Kind: inputmodel.Stdin, Reader: strings.NewReader(source), Name: "workflow.yml"})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Rule != "shell-name" {
					continue
				}
				count++
				if diagnostic.Code != tc.code || diagnostic.Severity != tc.severity {
					t.Fatalf("diagnostic: %+v", diagnostic)
				}
				if diagnostic.Start.Line != 6 {
					t.Fatalf("wrong source: %+v", diagnostic)
				}
			}
			want := 0
			if tc.code != "" {
				want = 1
			}
			if count != want {
				t.Fatalf("got %d shell findings, want %d: %+v", count, want, result.Diagnostics)
			}
		})
	}
}
