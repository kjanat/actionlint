package actionlint_fuzz

import (
	"testing"

	"actionlint.kjanat.dev"
)

func parseWorkflowPanicFree(data []byte) *actionlint.Workflow {
	// Avoid Parse() panicking. It panics when go-yaml panics
	defer func() { _ = recover() }()
	w, _ := actionlint.Parse(data)
	return w
}

func FuzzCheck(f *testing.F) {
	f.Add([]byte(`on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`))
	f.Add([]byte(`on: [push]
jobs:
  t:
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
`))

	f.Fuzz(func(_ *testing.T, data []byte) {
		w := parseWorkflowPanicFree(data)
		if w == nil {
			return
		}

		ac := actionlint.NewLocalActionsCache(nil, nil)
		wc := actionlint.NewLocalReusableWorkflowCache(nil, "", nil)

		rules := []actionlint.Rule{
			actionlint.NewRuleMatrix(),
			actionlint.NewRuleCredentials(),
			actionlint.NewRuleShellName(),
			actionlint.NewRuleRunnerLabel(),
			actionlint.NewRuleEvents(),
			actionlint.NewRuleGlob(),
			actionlint.NewRuleJobNeeds(),
			actionlint.NewRuleAction(ac),
			actionlint.NewRuleEnvVar(),
			actionlint.NewRuleID(),
			actionlint.NewRuleExpression(ac, wc),
			actionlint.NewRuleWorkflowCall("test.yaml", wc),
			actionlint.NewRulePermissions(),
			actionlint.NewRuleDeprecatedCommands(),
			actionlint.NewRuleIfCond(),
		}

		v := actionlint.NewVisitor()
		for _, rule := range rules {
			v.AddPass(rule)
		}

		_ = v.Visit(w)
	})
}
