package diagnosticpolicy

import (
	"strings"
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/rule"
)

func TestForeignSourcePolicyUsesSelectedToolRole(t *testing.T) {
	descriptor := rule.Descriptor{Name: "shellcheck", Category: "correctness", Provider: "shellcheck-rs"}
	for _, tc := range []struct {
		name, replaces string
		wantError      bool
	}{
		{name: "selected tool", replaces: "shellcheck"},
		{name: "ordinary analyzer", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := Policy{Path: "workflow.yml", Root: ".", RuleRegistrations: []repoconfig.RuleRegistration{{Descriptor: descriptor, ReplacesTool: tc.replaces}}}
			_, err := Apply(policy, nil, map[string][]byte{"action.yml": []byte("name: action\n")}, nil)
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), "conflicts") {
					t.Fatalf("expected collision, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
