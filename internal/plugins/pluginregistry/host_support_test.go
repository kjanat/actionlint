package pluginregistry

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestVersionHostSupport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		version Version
		want    SupportStatus
		reason  string
	}{
		{"no lists", Version{}, SupportUnknown, ""},
		{"exports only", Version{Exports: []string{component.AnalyzerInterface}}, SupportUnknown, ""},
		{"imports only", Version{Imports: []string{}}, SupportUnknown, ""},
		{"compatible", Version{Imports: []string{}, Exports: []string{component.AnalyzerInterface}}, SupportCompatible, ""},
		{"incompatible", Version{Imports: []string{"wasi:sockets/tcp@0.2.0"}, Exports: []string{component.AnalyzerInterface}}, SupportIncompatible, "wasi:sockets/tcp@0.2.0"},
	} {
		support := tc.version.HostSupport()
		if support.Status != tc.want || tc.reason == "" && support.Reason != "" || !strings.Contains(support.Reason, tc.reason) {
			t.Errorf("%s: %+v, want %s with %q", tc.name, support, tc.want, tc.reason)
		}
	}
}
