package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Check the dependency graph, so a moved facade cannot conceal an old owner.
func TestAnalysisOwnership(t *testing.T) {
	boundaries := map[string][]string{
		"engine":               {"application", "builtin", "checks", "config", "tools", "analyzerhost", "workspace", "diagnosticpolicy", "documentparse", "report", "workflow/composition"},
		"documentparse":        {"engine", "application", "builtin", "checks", "tools", "analyzerhost", "workspace", "config", "diagnosticpolicy"},
		"checks":               {"engine", "application", "tools", "analyzerhost", "workspace", "diagnosticpolicy", "render"},
		"tools/execution":      {"engine", "application", "workspace", "builtin", "checks", "diagnosticpolicy", "render"},
		"workflow/composition": {"application", "workspace", "diagnosticpolicy", "tools/registry", "tools/backend", "tools/managed", "tools/provider/process", "render"},
	}
	args := []string{"list", "-json", "actionlint.kjanat.dev/v2/document"}
	for name := range boundaries {
		args = append(args, "actionlint.kjanat.dev/v2/internal/"+name)
	}
	output, err := exec.CommandContext(t.Context(), "go", args...).Output()
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
		name := strings.TrimPrefix(pkg.ImportPath, "actionlint.kjanat.dev/v2/internal/")
		forbidden := boundaries[name]
		if pkg.ImportPath == "actionlint.kjanat.dev/v2/document" {
			name = "document"
			forbidden = []string{"engine", "application", "config", "analyzerhost", "workspace", "tools", "report", "documentparse"}
		}
		for _, dependency := range pkg.Deps {
			if isProviderData(dependency) {
				continue
			}
			for _, domain := range forbidden {
				prefix := "actionlint.kjanat.dev/v2/internal/" + domain
				if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
					t.Errorf("%s imports responsibility %s", name, dependency)
				}
			}
			if name == "engine" || name == "documentparse" || name == "checks" || name == "document" {
				if dependency == "os/exec" {
					t.Errorf("%s launches processes", name)
				}
			}
		}
	}
}
