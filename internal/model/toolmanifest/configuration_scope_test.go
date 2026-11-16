package toolmanifest

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/rule"
)

func TestConfigurationScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scope   Scope
		want    rule.ConfigurationScope
		invalid bool
	}{
		{ScopeSource, rule.SourceConfiguration, false},
		{ScopeComposite, rule.CompositeConfiguration, false},
		{"unknown", 0, true},
		{"", 0, true},
	} {
		t.Run(string(tc.scope), func(t *testing.T) {
			t.Parallel()
			got, err := tc.scope.ConfigurationScope()
			if (err != nil) != tc.invalid || !tc.invalid && got != tc.want {
				t.Fatalf("ConfigurationScope(%q) = %v, %v; want %v, invalid %v", tc.scope, got, err, tc.want, tc.invalid)
			}
		})
	}
}
