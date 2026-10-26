package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestInvocationProjectionDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/invocationview", "actionlint.kjanat.dev/v2/analyzer", "actionlint.kjanat.dev/v2/diagnostic").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath    string
			Imports, Deps []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.ImportPath == "actionlint.kjanat.dev/v2/internal/invocationview" {
			for _, imported := range pkg.Imports {
				if !strings.Contains(strings.Split(imported, "/")[0], ".") {
					continue
				}
				switch imported {
				case "actionlint.kjanat.dev/v2/analyzer", "actionlint.kjanat.dev/v2/diagnostic", "actionlint.kjanat.dev/v2/source", "actionlint.kjanat.dev/v2/internal/workflow/invocation":
				default:
					t.Errorf("projection imports implementation %s", imported)
				}
			}
		} else {
			for _, dependency := range pkg.Deps {
				if strings.HasPrefix(dependency, "actionlint.kjanat.dev/v2/internal/") {
					t.Errorf("public contract %s imports implementation %s", pkg.ImportPath, dependency)
				}
			}
		}
	}
}
