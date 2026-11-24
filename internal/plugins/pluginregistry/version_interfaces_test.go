package pluginregistry

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/plugins/pluginexecution/component"
)

func TestVersionInterfacesNeedBothLists(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		version Version
		listed  bool
	}{
		{"no lists", Version{}, false},
		{"exports only", Version{Exports: []string{component.AnalyzerInterface}}, false},
		{"imports only", Version{Imports: []string{}}, false},
		{"both lists", Version{Imports: []string{}, Exports: []string{component.AnalyzerInterface}}, true},
	} {
		interfaces, ok := tc.version.interfaces()
		if ok != tc.listed || ok && !slices.Equal(interfaces.Exports, tc.version.Exports) {
			t.Errorf("%s: interfaces=%+v listed=%t", tc.name, interfaces, ok)
		}
	}
}
