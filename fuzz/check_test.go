package actionlint_fuzz

import (
	"testing"

	"github.com/kjanat/actionlint"
)

func parseWorkflowPanicFree(data []byte) *actionlint.Workflow {
	// Avoid Parse() panicking. It panics when go-yaml panics
	defer func() { _ = recover() }()
	w, _ := actionlint.Parse(data)
	return w
}

func FuzzCheck(f *testing.F) {
	f.Add([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"))
	f.Add([]byte("on: [push]\njobs:\n  t:\n    runs-on: ${{ matrix.os }}\n    steps:\n      - uses: actions/checkout@v4\n"))

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
