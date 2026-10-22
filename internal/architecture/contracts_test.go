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
		"actionlint.kjanat.dev/internal/config",
		"actionlint.kjanat.dev/internal/config/tools/ruff",
		"actionlint.kjanat.dev/internal/render",
		"actionlint.kjanat.dev/internal/plugins/component",
		"actionlint.kjanat.dev/internal/plugins/core",
		"actionlint.kjanat.dev/plugin",
		"actionlint.kjanat.dev/internal/expression",
		"actionlint.kjanat.dev/internal/workflow",
		"actionlint.kjanat.dev/internal/expression/context",
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
// repository configuration. Import dependencies enforce these boundaries.
func TestExpressionDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/expression").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, domain := range []string{"workflow", "analyzerhost", "tools", "config", "engine", "application", "workspace", "render"} {
			prefix := "actionlint.kjanat.dev/internal/" + domain
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("expression semantics imports host domain %s", dependency)
			}
		}
		if dependency == "actionlint.kjanat.dev/analyzer" {
			t.Errorf("expression semantics imports analyzer execution %s", dependency)
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
		case dependency == "actionlint.kjanat.dev/internal/plugins/component", strings.HasPrefix(dependency, "github.com/partite-ai/wacogo"), strings.HasPrefix(dependency, "github.com/tetratelabs/wazero"):
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
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/plugins/component", "actionlint.kjanat.dev/internal/plugins/core":
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
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/workspace", "actionlint.kjanat.dev/internal/config":
			t.Errorf("analyzer scheduling imports traversal or input selection %s", dependency)
		}
	}
}

func TestSemanticChecksAndYAMLBoundary(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/internal/semantics/...", "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/workflow", "actionlint.kjanat.dev/internal/mapping").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath                               string
			Imports, Deps, TestImports, XTestImports []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, dependency := range pkg.Imports {
			if dependency == "go.yaml.in/yaml/v4" {
				t.Errorf("semantic package %s imports parser types", pkg.ImportPath)
			}
		}
		if strings.HasPrefix(pkg.ImportPath, "actionlint.kjanat.dev/internal/semantics") {
			for _, dependency := range append(append(pkg.Deps, pkg.TestImports...), pkg.XTestImports...) {
				if strings.HasPrefix(dependency, "go.yaml.in/") || strings.HasPrefix(dependency, "gopkg.in/yaml.") {
					t.Errorf("semantic check %s depends on YAML decoding %s", pkg.ImportPath, dependency)
				}
				for _, domain := range []string{"workflow", "action", "builtin", "engine", "config", "workspace", "analyzerhost", "tools", "render"} {
					prefix := "actionlint.kjanat.dev/internal/" + domain
					if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
						t.Errorf("semantic check %s depends on adapter or host %s", pkg.ImportPath, dependency)
					}
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
			case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/cli", "actionlint.kjanat.dev/internal/localapp", "actionlint.kjanat.dev/internal/plugins/component", "actionlint.kjanat.dev/internal/config":
				t.Errorf("%s imports host implementation %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// Configuration schemas are generated from their owning model and catalogue;
// loading an analysis engine to discover configuration metadata breaks that boundary.
func TestConfigurationGeneratorDoesNotImportEngine(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/tools/config").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/cli" {
			t.Errorf("configuration generator imports host implementation %s", dependency)
		}
	}
}

// The result model and semantic execution must remain independent of presentation.
// The test checks transitive dependencies so a facade cannot hide a reversed edge.
func TestAnalysisDoesNotDependOnPresentation(t *testing.T) {
	for _, name := range []string{"internal/report", "internal/engine", "internal/application", "analysis"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if dependency == "actionlint.kjanat.dev/internal/render" || dependency == "github.com/fatih/color" || dependency == "github.com/spf13/cobra" {
				t.Errorf("%s depends on presentation through %s", name, dependency)
			}
		}
	}
}

func TestLanguagePreparationDoesNotDependOnExecution(t *testing.T) {
	for _, name := range []string{"internal/workflow/facts", "internal/workflow/checks", "internal/workflow/schema", "internal/workflow/references"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			switch dependency {
			case "os/exec", "actionlint.kjanat.dev/internal/analyzerhost", "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/config":
				t.Errorf("%s depends on execution/configuration through %s", name, dependency)
			}
		}
	}
}

// Dependency metadata may read repository sources, but must not construct a
// rule host, launch a checker, or render diagnostics to parse those sources.
func TestWorkflowDependencyMetadataDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/workflow/calls").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/internal/engine", "actionlint.kjanat.dev/internal/application", "actionlint.kjanat.dev/internal/analyzerhost", "actionlint.kjanat.dev/internal/render", "actionlint.kjanat.dev/internal/tools/adapters", "actionlint.kjanat.dev/internal/config", "actionlint.kjanat.dev/internal/workspace":
			t.Errorf("workflow dependency metadata imports host %s", dependency)
		}
	}
}

func TestEntrypointsUseApplicationBoundary(t *testing.T) {
	for _, name := range []string{"internal/cli", "internal/githubaction", "analysis", "internal/rulecatalog/check-docs"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, "actionlint.kjanat.dev/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if dependency == "actionlint.kjanat.dev/internal/engine" {
				t.Errorf("%s bypasses application ownership", name)
			}
		}
	}
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, "actionlint.kjanat.dev/internal/engine").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if dependency == "os/exec" {
			t.Error("engine owns native process execution")
		}
	}
}

func TestToolIntegrationDependencyDirection(t *testing.T) {
	for _, name := range []string{"engine", "application", "tools", "tools/adapters", "tools/backend", "tools/ruff", "tools/shellcheck", "tools/selection"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if name == "engine" || name == "application" {
				if dependency == "actionlint.kjanat.dev/internal/tools/adapters" || dependency == "actionlint.kjanat.dev/internal/tools/ruff" || dependency == "actionlint.kjanat.dev/internal/tools/backend" || dependency == "actionlint.kjanat.dev/internal/tools/shellcheck" || dependency == "actionlint.kjanat.dev/internal/tools/selection" {
					t.Errorf("%s depends on tool implementation %s", name, dependency)
				}
			} else if dependency == "actionlint.kjanat.dev/internal/engine" || dependency == "actionlint.kjanat.dev/internal/application" {
				t.Errorf("%s depends on host %s", name, dependency)
			}
		}
	}
}

// Observations may be consumed by more than one language adapter. They must not
// acquire semantic policy through a dependency on the expression or workflow code.
func TestWebhookObservationsHaveNoSemanticDependencies(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/webhook/payload").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if strings.HasPrefix(dependency, "actionlint.kjanat.dev/") {
			t.Errorf("webhook observations import semantic dependency %s", dependency)
		}
	}
}

// Extracted language and suppression domains return values to the host; neither
// direct imports nor aliases may pull orchestration, policy loading or tools back in.
func TestExtractedDomainsDoNotImportHost(t *testing.T) {
	packages := []string{
		"actionlint.kjanat.dev/internal/action",
		"actionlint.kjanat.dev/internal/action/validation",
		"actionlint.kjanat.dev/internal/action/local",
		"actionlint.kjanat.dev/internal/fsread",
		"actionlint.kjanat.dev/internal/suppression",
		"actionlint.kjanat.dev/internal/workflow/matrix",
		"actionlint.kjanat.dev/internal/workflow/cachepolicy",
		"actionlint.kjanat.dev/internal/workflow/names",
		"actionlint.kjanat.dev/internal/actioncatalog",
		"actionlint.kjanat.dev/internal/workflow/runner",
		"actionlint.kjanat.dev/internal/workflow/execution",
		"actionlint.kjanat.dev/internal/workflow/expressions",
		"actionlint.kjanat.dev/internal/workflow/visit",
		"actionlint.kjanat.dev/internal/finding",
		"actionlint.kjanat.dev/internal/actioncatalog/metadata",
		"actionlint.kjanat.dev/internal/actioncatalog/generate",
	}
	output, err := exec.CommandContext(t.Context(), "go", append([]string{"list", "-json"}, packages...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath                      string
			Deps, TestImports, XTestImports []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, dependency := range append(append(pkg.Deps, pkg.TestImports...), pkg.XTestImports...) {
			for _, forbidden := range []string{"internal/engine", "internal/application", "internal/cli", "internal/workspace", "internal/config", "internal/analyzerhost", "internal/tools", "internal/render"} {
				prefix := "actionlint.kjanat.dev/" + forbidden
				if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
					t.Errorf("domain %s imports host implementation %s", pkg.ImportPath, dependency)
				}
			}
		}
	}
}

func TestDiagnosticPolicyDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/diagnosticpolicy").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, forbidden := range []string{"engine", "application", "cli", "workspace", "analyzerhost", "tools", "render"} {
			prefix := "actionlint.kjanat.dev/internal/" + forbidden
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("diagnostic policy imports host %s", dependency)
			}
		}
	}
}

// Built-in registration owns concrete adapters; it receives observations through
// callbacks and cannot construct application sessions or execution transports.
func TestBuiltinCompositionDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/builtin").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, forbidden := range []string{"engine", "application", "cli", "workspace", "analyzerhost", "tools", "render"} {
			prefix := "actionlint.kjanat.dev/internal/" + forbidden
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("built-in composition imports host implementation %s", dependency)
			}
		}
		if dependency == "os/exec" {
			t.Error("built-in composition launches processes")
		}
	}
}

// The engine consumes registration and visitor contracts instead of constructing
// semantic implementations. Domain dependencies remain behind composition.
func TestEngineUsesBuiltinComposition(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Imports "\n"}}`, "actionlint.kjanat.dev/internal/engine").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, domain := range []string{"semantics", "action/validation", "workflow/checks", "workflow/expressions", "workflow/cachepolicy", "workflow/matrix", "workflow/commands"} {
			prefix := "actionlint.kjanat.dev/internal/" + domain
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("engine constructs semantic checks through %s", dependency)
			}
		}
	}
}

// Bundling must not turn the two native integrations into a shared implementation.
func TestBundledToolsHaveIndependentImplementations(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/internal/tools/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			prefix := "actionlint.kjanat.dev/internal/tools/"
			if strings.HasPrefix(dependency, prefix) && dependency != prefix+"selection" {
				t.Errorf("tool %s imports another execution implementation %s", name, dependency)
			}
		}
	}
}

// Integration suites must exercise the application boundary, without reopening
// the engine's private state through aliases or a replacement test facade.
func TestIntegrationSuitesUseApplicationBoundary(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/internal/integration/...").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath                         string
			Imports, TestImports, XTestImports []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, dependency := range append(append(pkg.Imports, pkg.TestImports...), pkg.XTestImports...) {
			if dependency == "actionlint.kjanat.dev/internal/engine" {
				t.Errorf("%s bypasses the application boundary", pkg.ImportPath)
			}
		}
	}
}
