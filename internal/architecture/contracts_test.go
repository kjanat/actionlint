package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Report and extension contracts must not depend on the application that uses
// them. This checks transitive imports, including dependencies hidden by aliases.
func TestLeafContractsDoNotImportEngine(t *testing.T) {
	packages := []string{"actionlint.kjanat.dev/analyzer", "actionlint.kjanat.dev/diagnostic", "actionlint.kjanat.dev/document"}
	output, err := exec.CommandContext(t.Context(), "go", append([]string{"list", "-json"}, packages...)...).Output()
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
			if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/cli" || dependency == "os/exec" {
				t.Errorf("contract %s imports application dependency %s", pkg.ImportPath, dependency)
			}
		}
	}
}
