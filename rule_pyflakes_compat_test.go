package actionlint_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev"
)

var _ actionlint.Rule = (*actionlint.RulePyflakes)(nil)

func TestRulePyflakesCompatibility(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"", executable, filepath.Join(t.TempDir(), "missing-pyflakes"), "invalid 'command", "\x00"} {
		t.Run(command, func(t *testing.T) {
			rule, err := actionlint.NewRulePyflakes(command, nil)
			if err != nil || rule == nil {
				t.Fatalf("retired constructor rejected ignored command: %v, %v", rule, err)
			}
			if rule.Name() != "pyflakes" {
				t.Fatalf("rule name changed: %q", rule.Name())
			}
			seen := false
			linter, err := actionlint.NewLinter(io.Discard, &actionlint.LinterOptions{
				Shellcheck: "",
				OnRulesCreated: func(rules []actionlint.Rule) []actionlint.Rule {
					rules = append(rules, rule)
					for _, candidate := range rules {
						switch candidate.(type) {
						case *actionlint.RulePyflakes:
							seen = true
						}
					}
					return rules
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			const workflow = "on: push\ndefaults: {run: {shell: python}}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: print(undefined_name)\n"
			findings, err := linter.Lint("workflow.yaml", []byte(workflow), nil)
			if err != nil || len(findings) != 0 || len(rule.Errs()) != 0 || !seen {
				t.Fatalf("retired rule changed analysis: seen=%v, findings=%v, error=%v", seen, findings, err)
			}
		})
	}
}
