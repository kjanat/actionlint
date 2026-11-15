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
			policy := resultpolicy.Policy{Path: "workflow.yml", Root: "."}
			if _, err := publishFindings(t, policy, nil, nil, map[string][]byte{"action.yml": []byte("name: action\n")}, map[string]*repoconfig.Config{"action.yml": cfg}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublicationRejectsUnpreparedForeignSource(t *testing.T) {
	t.Parallel()
	batch := preparePublicationBatch(t, resultpolicy.Batch{InputPath: "workflow.yml", Sources: map[string][]byte{"action.yml": []byte("name: action\n")}}, nil, nil)
	_, err := resultpolicy.Publish([]resultpolicy.Batch{batch}, nil)
	if err == nil || !strings.Contains(err.Error(), "rule configuration was not prepared for source") {
		t.Fatalf("missing source binding was accepted: %v", err)
	}
}
