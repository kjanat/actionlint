package policy_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestSourcePolicyRequiresPreparedConfiguration(t *testing.T) {
	t.Parallel()
	processor := policy.Processor{RuleSet: checking.ConfigurationRules()}
	sources := map[string][]byte{"inner/action.yml": []byte("name: inner\n")}
	if _, err := processor.Apply(policy.Policy{Path: "workflow.yml"}, nil, sources, nil); err == nil || !strings.Contains(err.Error(), "configuration was not prepared") {
		t.Fatalf("missing configuration did not fail: %v", err)
	}
	prepared := policy.Policy{Path: "workflow.yml", SourceConfigurations: map[string]*repoconfig.Config{"inner/action.yml": nil}}
	if _, err := processor.Apply(prepared, nil, sources, nil); err != nil {
		t.Fatalf("explicit default configuration was rejected: %v", err)
	}
}
