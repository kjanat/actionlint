package policy_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/results/policy"
)

func TestSourcePolicyRequiresPreparedConfiguration(t *testing.T) {
	t.Parallel()
	processor := policy.Processor{RuleSet: checking.ConfigurationRules()}
	sources := map[string][]byte{"inner/action.yml": []byte("name: inner\n")}
	missing := preparePolicy(processor.RuleSet, nil, policy.DocumentInputs{Path: "workflow.yml"}, nil, nil)
	if _, err := publishFindings(processor, missing, nil, sources); err == nil || !strings.Contains(err.Error(), "configuration was not prepared") {
		t.Fatalf("missing configuration did not fail: %v", err)
	}
	prepared := preparePolicy(processor.RuleSet, nil, policy.DocumentInputs{Path: "workflow.yml"}, map[string]*configuration.Config{"inner/action.yml": nil}, nil)
	if _, err := publishFindings(processor, prepared, nil, sources); err != nil {
		t.Fatalf("explicit default configuration was rejected: %v", err)
	}
}
