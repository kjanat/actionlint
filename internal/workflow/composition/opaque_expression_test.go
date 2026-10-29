package composition

import (
	"bytes"
	"fmt"
	"testing"

	"actionlint.kjanat.dev/v2/internal/builtin"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

func TestOpaqueCompositeInputsKeepExpressionFindingsAfterCycleFrontier(t *testing.T) {
	for _, standalone := range []bool{false, true} {
		for _, cycle := range []bool{false, true} {
			for _, sink := range []struct{ expression, rule string }{{"github.event.issue.title", "script-injection"}, {"github.repository", "script-interpolation"}} {
				t.Run(fmt.Sprintf("standalone=%t/cycle=%t/%s", standalone, cycle, sink.rule), func(t *testing.T) {
					content := "name: outer\ndescription: test\nruns:\n  using: composite\n  steps:\n"
					if cycle {
						content += "    - uses: $/outer\n"
					}
					content += "    - uses: actions/github-script@v7\n      with:\n        script: ${{ " + sink.expression + " }}\n"
					fixture := newInvocationFixture(t, t.Context(), "on: issues\njobs: {test: {runs-on: ubuntu-latest, steps: [{uses: $/outer}]}}\n", map[string]string{"outer/action.yml": content}, nil)
					fixture.service.options.Rules = []workflowvisit.Rule{builtin.NewRuleExpression(fixture.service.options.Actions, nil)}
					var result Result
					var err error
					if standalone {
						lookup, _ := fixture.cache.LookupMetadata("$/outer")
						if lookup.Metadata() == nil {
							t.Fatal("standalone action metadata absent")
						}
						result, err = standaloneActionForTest(t, fixture.service, lookup.Metadata())
					} else {
						result, err = fixture.service.Workflow(fixture.root)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					matched := 0
					for _, finding := range result.Findings {
						if finding.Rule != sink.rule {
							continue
						}
						matched++
						if finding.Path == "" || !bytes.Equal(finding.Source, []byte(content)) || finding.Reference == nil || finding.Reference.Document == fixture.root.Reference().Document || finding.Start.Line < 1 || finding.Start.Column < 1 {
							t.Fatalf("opaque input lost its declaring source: %+v", finding)
						}
					}
					cycles := 0
					for _, call := range result.Batch.Invocations().Calls() {
						if call.Outcome.Kind() == invocation.OutcomeCycle {
							cycles++
						}
					}
					if matched != 1 || cycle && cycles != 1 || !cycle && cycles != 0 {
						t.Fatalf("opaque sink or cycle frontier changed: findings=%+v cycles=%d", result.Findings, cycles)
					}
				})
			}
		}
	}
}
