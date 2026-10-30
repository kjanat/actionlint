package architecture_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// providerData lists the tool provider packages that hold data only: the
// bundled manifests and the compiler for the configuration schemas they
// declare. The rule catalog and configuration decoding read them, so boundary
// checks that keep tool hosts out of a package admit these two.
var providerData = []string{
	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled",
	"actionlint.kjanat.dev/v2/internal/tools/provider/configschema",
}

func isProviderData(dependency string) bool {
	return slices.Contains(providerData, dependency)
}

// The exemption holds only while provider data reaches no other module package.
func TestProviderDataIsLeaf(t *testing.T) {
	for _, data := range providerData {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, data).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if dependency == "actionlint.kjanat.dev/v2" || strings.HasPrefix(dependency, "actionlint.kjanat.dev/v2/") {
				t.Errorf("provider data %s imports %s", data, dependency)
			}
		}
	}
}
