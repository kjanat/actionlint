package manifests

import (
	"testing"

	rulemeta "actionlint.kjanat.dev/v2/internal/model/rules"
)

func TestConfigurationScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scope   Scope
		want    rulemeta.ConfigurationScope
		invalid bool
	}{
		{ScopeSource, rulemeta.SourceConfiguration, false},
		{ScopeComposite, rulemeta.CompositeConfiguration, false},
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
