package architecture_test

import (
	"os/exec"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider/bundled"
)

// Tools are data: bundled providers are manifests under
// internal/tools/provider/bundled, so no package below internal/tools is
// named for one of them.
func TestNoPackageIsNamedForABundledTool(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "actionlint.kjanat.dev/v2/internal/tools/...").Output()
	if err != nil {
		t.Fatal(err)
	}
	for path := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(path, "actionlint.kjanat.dev/v2/internal/tools/provider/bundled") {
			continue
		}
		for element := range strings.SplitSeq(path, "/") {
			if slices.Contains(bundled.Names(), element) {
				t.Errorf("package %s is named for the bundled tool %s", path, element)
			}
		}
	}
}
