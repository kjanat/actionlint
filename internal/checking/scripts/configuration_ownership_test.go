package scripts

import (
	"context"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	config "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/model/lifecycle"
	"actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

type configurationRunner func(tools.Batch)

func (run configurationRunner) Analyze(_ context.Context, batch tools.Batch) ([]tools.Finding, error) {
	run(batch)
	return nil, nil
}

func TestRegisteredRunnerCannotMutatePreparedPolicyOrSibling(t *testing.T) {
	t.Parallel()
	rules, err := config.NewRuleSet([]rule.Declaration{{Name: "runtime-check", Category: "correctness", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.Parse([]byte(`lint:
  enabled: true
  lifecycle:
    policies:
      - id: retirement
        description: Prepare for retirement
        match: {subjects: [language-runtimes], events: [support-ends]}
        notifications:
          - {starts: {before: 90d}, severity: warning}
`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = rules.ResolveRules(config.Presets{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	registry := binding.NewRegistry(rules)
	scope := registry.Prepare(cfg, cfg, cfg)
	graph, collection := batchSourceFixture(t, []byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo safe\n"))
	collection.Scopes[0].Config, collection.Scopes[0].Bindings = cfg, scope
	batch, err := NewBatch(graph, collection)
	if err != nil {
		t.Fatal(err)
	}
	asOf, _ := lifecycle.ParseDate("2026-11-01")
	effective, _ := lifecycle.ParseDate("2026-12-01")
	publication := policy.Batch{
		Processor: policy.Processor{RuleSet: rules},
		Policy:    policy.Policy{Path: "ci.yml", Scope: scope, Bindings: registry.Seal()},
		Findings: []*finding.Finding{{
			Rule: "runtime-check", Message: "future retirement", Binding: scope.Rule("runtime-check").ID(),
			LifecycleEvidence: &lifecycle.Evidence{Subject: lifecycle.LanguageRuntimes, Kind: lifecycle.SupportEnds, Name: "example", AsOf: asOf, Effective: effective},
		}},
	}
	before, err := policy.Publish([]policy.Batch{publication}, nil)
	if err != nil || len(before.Diagnostics) != 1 || before.Diagnostics[0].Severity != "warning" {
		t.Fatalf("baseline policy did not publish the future event: %+v, %v", before.Diagnostics, err)
	}
	mutating := Tool{Name: "runtime-check", Runner: configurationRunner(func(input tools.Batch) {
		input.Scopes[0].Config.Lint.Lifecycle.Policies[0].Notifications[0].Severity = lifecycle.Error
		*input.Scripts[0].Config.Lint.Enabled = false
	})}
	if _, err := mutating.AnalyzeScripts(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	observing := Tool{Name: "runtime-check", Runner: configurationRunner(func(input tools.Batch) {
		selected := input.Scopes[0].Config
		if !*selected.Lint.Enabled || selected.Lint.Lifecycle.Policies[0].Notifications[0].Severity != lifecycle.Warning {
			t.Error("a sibling runner observed another runner's configuration mutation")
		}
	})}
	if _, err := observing.AnalyzeScripts(t.Context(), batch); err != nil {
		t.Fatal(err)
	}
	after, err := policy.Publish([]policy.Batch{publication}, nil)
	if err != nil || len(after.Diagnostics) != 1 || after.Diagnostics[0].Severity != "warning" {
		t.Fatalf("runner mutation changed future policy publication: %+v, %v", after.Diagnostics, err)
	}
}
