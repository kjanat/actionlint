package config

import (
	"sync"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
)

func TestConcurrentRuleBindingOwnsMutableTargets(t *testing.T) {
	t.Parallel()
	rules, err := NewRuleSet([]rule.Declaration{{Name: "example", Aliases: []rule.Alias{{Name: "old-example"}}, Category: "correctness", Maturity: "stable", Recommended: true}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := rules.Parse([]byte("overrides:\n  - includes: ['**']\n    lint: {rules: {correctness: {example: on}}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Policy.DisallowSuppressions, err = rules.DisallowSuppressions("all", "old-example")
	if err != nil {
		t.Fatal(err)
	}
	policy, override := cfg.Policy.DisallowSuppressions, cfg.Overrides[0].Lint
	start := make(chan struct{})
	var done sync.WaitGroup
	for range 32 {
		done.Go(func() {
			<-start
			resolved, err := rules.ResolveRules(Presets{}, cfg)
			if err != nil {
				t.Error(err)
				return
			}
			if resolved.Policy.DisallowSuppressions == policy || resolved.Overrides[0].Lint == override {
				t.Error("resolved configuration shares mutable binding targets with its input")
			}
			if got := resolved.Policy.DisallowSuppressions.ReportFor("example"); got != ReportAll {
				t.Errorf("bound suppression alias reports %v, want %v", got, ReportAll)
			}
		})
	}
	close(start)
	done.Wait()
	if cfg.Policy.DisallowSuppressions != policy || cfg.Overrides[0].Lint != override {
		t.Fatal("binding replaced caller-owned configuration fields")
	}
}
