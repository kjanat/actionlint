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
		"actionlint.kjanat.dev/internal/corewasm",
		"actionlint.kjanat.dev/plugin",
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
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/application", "actionlint.kjanat.dev/analysis").Output()
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

// The terminal adapter must consume application and presentation contracts,
// never the engine's sessions, traversal objects, or concrete plugin runtimes.
func TestCLIDoesNotImportEngine(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, "actionlint.kjanat.dev/internal/cli").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/component", "actionlint.kjanat.dev/internal/corewasm":
			t.Errorf("CLI imports implementation %s", dependency)
		}
	}
}

func TestApplicationDoesNotImportCLI(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/application", "actionlint.kjanat.dev/internal/localapp").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/cli", "github.com/spf13/cobra", "github.com/spf13/pflag":
			t.Errorf("application imports terminal adapter %s", dependency)
		}
	}
}

// Input preparation must be usable without parsing, rules, or analyzer runtimes.
func TestWorkspaceDoesNotImportAnalysis(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/workspace").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/application", "actionlint.kjanat.dev/internal/cli", "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/analyzerhost", "actionlint.kjanat.dev/analyzer":
			t.Errorf("input preparation imports analysis implementation %s", dependency)
		}
	}
}

func TestAnalyzerHostDoesNotImportTraversal(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/analyzerhost").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/workspace", "actionlint.kjanat.dev/internal/repoconfig":
			t.Errorf("analyzer scheduling imports traversal or input selection %s", dependency)
		}
	}
}

func TestSemanticChecksAndYAMLBoundary(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/internal/checks/...", "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/mapping").Output()
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
		for _, dependency := range pkg.Imports {
			if dependency == "go.yaml.in/yaml/v4" {
				t.Errorf("semantic package %s imports parser types", pkg.ImportPath)
			}
		}
		if strings.Contains(pkg.ImportPath, "/internal/checks/") {
			for _, dependency := range pkg.Deps {
				if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/repoconfig" || dependency == "actionlint.kjanat.dev/internal/workflow" {
					t.Errorf("check %s depends on traversal/configuration %s", pkg.ImportPath, dependency)
				}
			}
		}
	}
}

func TestNewContractsDoNotImportTheirImplementations(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/rule", "actionlint.kjanat.dev/source", "actionlint.kjanat.dev/resolver", "actionlint.kjanat.dev/internal/langserver", "actionlint.kjanat.dev/internal/fix", "actionlint.kjanat.dev/internal/rulecatalog").Output()
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
			switch dependency {
			case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/cli", "actionlint.kjanat.dev/internal/localapp", "actionlint.kjanat.dev/internal/component", "actionlint.kjanat.dev/internal/repoconfig":
				t.Errorf("%s imports host implementation %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// Configuration schemas are generated from their owning model and catalogue;
// loading an analysis engine to discover configuration metadata breaks that boundary.
func TestConfigurationGeneratorDoesNotImportEngine(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/repoconfig/generate").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/cli" {
			t.Errorf("configuration generator imports host implementation %s", dependency)
		}
	}
}
