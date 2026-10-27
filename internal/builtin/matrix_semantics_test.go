package builtin_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func TestSuspiciousMatrixFilterSelection(t *testing.T) {
	for _, tc := range []struct {
		matrix string
		warn   bool
	}{
		{`value: ["0"], exclude: [{value: false}]`, true},
		{`value: [false], exclude: [{value: "0"}]`, true},
		{`value: [false], exclude: [{value: 0}]`, true},
		{`value: [0], exclude: [{value: false}]`, true},
		{`value: [null], exclude: [{value: 0}]`, true},
		{`value: [false], include: [{value: null}]`, true},
		{`value: [{nested: false}], exclude: [{value: {nested: 0}}]`, true},
		{`value: ["0x80000000"], include: [{value: 0}]`, true},
		{`value: "${{ fromJSON(vars.AXIS) }}", exclude: [{value: false}]`, true},
		{`value: "${{ fromJSON('[\"0\"]') }}", exclude: [{value: false}]`, true},
		{`value: [{nested: "0"}], exclude: [{value: {nested: false}}]`, true},
		{`value: [{}], exclude: [{value: {missing: ""}}]`, true},
		{`value: [[]], exclude: [{value: [""]}]`, true},
		{`value: [{}], exclude: [{value: {missing: null}}]`, false},
		{`value: [false], exclude: [{value: false}]`, false},
		{`value: [1], exclude: [{value: 1.0}]`, false},
		{`value: [0x10], exclude: [{value: 16.0}]`, false},
		{`value: [null], exclude: [{value: null}]`, false},
		{`value: [linux], exclude: [{value: LINUX}]`, false},
		{`value: "${{ fromJSON('[false]') }}", exclude: [{value: false}]`, false},
		{`value: "${{ fromJSON(vars.AXIS) }}", exclude: [{value: linux}]`, false},
		{`value: [linux], include: [{extra: false}]`, false},
	} {
		for _, config := range []string{"", "policy: {}", "policy: null", "lint: {rules: {suspicious: off}}", "lint: {rules: {suspicious: {mixed-type-matrix-filters: on}}}"} {
			t.Run(tc.matrix+"/"+config, func(t *testing.T) {
				_, snapshot := matrixTestWorkflow(t, `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix: {`+tc.matrix+`}
    steps:
      - run: echo ok
`)
				rule := builtin.NewRuleMatrix()
				if config != "" {
					cfg, err := repoconfig.Parse([]byte(config))
					if err != nil {
						t.Fatal(err)
					}
					rule.SetConfig(cfg)
				}
				if err := rule.VisitJobPre(matrixJob(t, snapshot, "test")); err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, err := range rule.Errs() {
					if strings.Contains(err.Message, "using Actions loose equality") {
						if err.Rule != "mixed-type-matrix-filters" || strings.Contains(err.Message, "policy") {
							t.Fatal(err)
						}
						count++
					}
				}
				if (count == 1) != (tc.warn && strings.Contains(config, ": on")) {
					t.Fatal(rule.Errs())
				}
			})
		}
	}
}
