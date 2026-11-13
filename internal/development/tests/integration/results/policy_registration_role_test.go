package results_test

import (
	"strings"
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	resultpolicy "actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestForeignSourcePolicyUsesSelectedToolRole(t *testing.T) {
	t.Parallel()
	descriptor := rule.Descriptor{Name: "shellcheck", Category: "correctness", Provider: "shellcheck-rs"}
	for _, tc := range []struct {
		name, replaces string
		wantError      bool
	}{
		{name: "selected tool", replaces: "shellcheck"},
		{name: "ordinary analyzer", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := checking.ConfigurationRules().ResolveRegisteredRules(repoconfig.Presets{}, nil, repoconfig.RuleRegistration{Descriptor: descriptor, ReplacesTool: tc.replaces})
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "conflicts") {
					t.Fatalf("expected collision, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			policy := resultpolicy.Policy{Path: "workflow.yml", Root: ".", SourceConfigurations: map[string]*repoconfig.Config{"action.yml": cfg}}
			if _, err := (resultpolicy.Processor{RuleSet: checking.ConfigurationRules()}).Apply(policy, nil, map[string][]byte{"action.yml": []byte("name: action\n")}, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
