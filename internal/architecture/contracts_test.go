package architecture_test

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Language implementations and public leaf contracts must not depend on their
// application host. Check transitive imports, including dependencies hidden by aliases.
func TestIndependentPackagesDoNotImportHost(t *testing.T) {
	packages := []string{
		"actionlint.kjanat.dev/analyzer",
		"actionlint.kjanat.dev/diagnostic",
		"actionlint.kjanat.dev/document",
		"actionlint.kjanat.dev/syntax",
		"actionlint.kjanat.dev/config",
		"actionlint.kjanat.dev/internal/repoconfig",
		"actionlint.kjanat.dev/internal/ruffconfig",
		"actionlint.kjanat.dev/internal/render",
		"actionlint.kjanat.dev/internal/component",
		"actionlint.kjanat.dev/internal/expression",
		"actionlint.kjanat.dev/internal/workflow",
		"actionlint.kjanat.dev/internal/workflowcontext",
		"actionlint.kjanat.dev/internal/yamlscalar",
		"actionlint.kjanat.dev/internal/stringutil",
	}
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
			if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/cli" || dependency == "os/exec" || dependency == "actionlint.kjanat.dev/analysis" || dependency == "actionlint.kjanat.dev/config" {
				t.Errorf("contract %s imports application dependency %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// Expression semantics must remain usable without workflow ASTs, execution or
// repository configuration. These are architectural boundaries, not file names.
func TestExpressionDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/expression").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/analyzerhost", "actionlint.kjanat.dev/analyzer", "actionlint.kjanat.dev/internal/ruff", "actionlint.kjanat.dev/internal/configtree":
			t.Errorf("expression semantics imports host domain %s", dependency)
		}
	}
}

func TestEngineDoesNotImportPluginRuntime(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/analysis").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch {
		case dependency == "actionlint.kjanat.dev/internal/component", strings.HasPrefix(dependency, "github.com/partite-ai/wacogo"), strings.HasPrefix(dependency, "github.com/tetratelabs/wazero"):
			t.Errorf("engine imports plugin runtime %s", dependency)
		}
	}
}
