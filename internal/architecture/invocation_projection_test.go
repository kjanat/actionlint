package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestInvocationProjectionDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/analyzer", "actionlint.kjanat.dev/v2/diagnostic").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Deps       []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, dependency := range pkg.Deps {
			if strings.HasPrefix(dependency, "actionlint.kjanat.dev/v2/internal/") {
				t.Errorf("public contract %s imports implementation %s", pkg.ImportPath, dependency)
			}
		}
	}
}
