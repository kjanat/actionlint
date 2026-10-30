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
		"actionlint.kjanat.dev/v2/analyzer",
		"actionlint.kjanat.dev/v2/runner",
		"actionlint.kjanat.dev/v2/diagnostic",
		"actionlint.kjanat.dev/v2/document",
		"actionlint.kjanat.dev/v2/syntax",
		"actionlint.kjanat.dev/v2/config",
		"actionlint.kjanat.dev/v2/config/report",
		"actionlint.kjanat.dev/v2/internal/config",
		"actionlint.kjanat.dev/v2/internal/config/tools/ruff",
		"actionlint.kjanat.dev/v2/internal/render",
		"actionlint.kjanat.dev/v2/internal/plugins/component",
		"actionlint.kjanat.dev/v2/internal/plugins/core",
		"actionlint.kjanat.dev/v2/plugin",
		"actionlint.kjanat.dev/v2/internal/expression",
		"actionlint.kjanat.dev/v2/internal/workflow",
		"actionlint.kjanat.dev/v2/internal/expression/context",
		"actionlint.kjanat.dev/v2/internal/yaml/schema",
		"actionlint.kjanat.dev/v2/internal/stringutil",
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
			if dependency == "actionlint.kjanat.dev/v2/internal/engine" || dependency == "actionlint.kjanat.dev/v2/internal/cli" || dependency == "os/exec" || dependency == "actionlint.kjanat.dev/v2/analysis" || dependency == "actionlint.kjanat.dev/v2/config" {
				t.Errorf("contract %s imports application dependency %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// A value contract must remain a leaf, and completed results and presentation
// must not acquire configuration loading or resolution through an indirect import.
func TestConfigurationReportDependencyDirection(t *testing.T) {
	const contract = "actionlint.kjanat.dev/v2/config/report"
	output, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "-json", contract).Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Standard   bool
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		if pkg.ImportPath != contract && !pkg.Standard {
			t.Errorf("configuration report contract imports nonstandard dependency %s", pkg.ImportPath)
		}
	}
	output, err = exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/report", "actionlint.kjanat.dev/v2/internal/render").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(strings.NewReader(string(output)))
	for decoder.More() {
		var pkg struct {
			ImportPath string
			Deps       []string
		}
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatal(err)
		}
		for _, dependency := range pkg.Deps {
			const implementation = "actionlint.kjanat.dev/v2/internal/config"
			if dependency == implementation || strings.HasPrefix(dependency, implementation+"/") {
				t.Errorf("%s imports configuration implementation %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// Expression semantics must remain usable without workflow ASTs, execution or
// repository configuration. Import dependencies enforce these boundaries.
func TestExpressionDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/expression").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, domain := range []string{"workflow", "analyzerhost", "tools", "config", "engine", "application", "workspace", "render"} {
			prefix := "actionlint.kjanat.dev/v2/internal/" + domain
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("expression semantics imports host domain %s", dependency)
			}
		}
		if dependency == "actionlint.kjanat.dev/v2/analyzer" {
			t.Errorf("expression semantics imports analyzer execution %s", dependency)
		}
	}
}

func TestInvocationOwnerDependencyDirection(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/workflow/invocation").Output()
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct{ Deps []string }
	if err := json.Unmarshal(output, &pkg); err != nil {
		t.Fatal(err)
	}
	for _, dependency := range pkg.Deps {
		if strings.Contains(strings.Split(dependency, "/")[0], ".") && dependency != "actionlint.kjanat.dev/v2/source" {
			t.Errorf("invocation owner imports implementation dependency %s", dependency)
		}
	}
}

func TestEngineDoesNotImportPluginRuntime(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/application", "actionlint.kjanat.dev/v2/analysis").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch {
		case dependency == "actionlint.kjanat.dev/v2/internal/plugins/component", strings.HasPrefix(dependency, "github.com/partite-ai/wacogo"), strings.HasPrefix(dependency, "github.com/kjanat/wacogo"), strings.HasPrefix(dependency, "github.com/tetratelabs/wazero"), strings.HasPrefix(dependency, "github.com/kjanat/wazero"):
			t.Errorf("engine imports plugin runtime %s", dependency)
		}
	}
}

func TestApplicationDoesNotImportCLI(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/application", "actionlint.kjanat.dev/v2/internal/localapp").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/v2/internal/cli", "github.com/spf13/cobra", "github.com/spf13/pflag":
			t.Errorf("application imports terminal adapter %s", dependency)
		}
	}
}

// Input preparation must be usable without parsing, rules, or analyzer runtimes.
func TestWorkspaceDoesNotImportAnalysis(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/workspace").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/application", "actionlint.kjanat.dev/v2/internal/cli", "actionlint.kjanat.dev/v2/internal/workflow", "actionlint.kjanat.dev/v2/internal/analyzerhost", "actionlint.kjanat.dev/v2/analyzer":
			t.Errorf("input preparation imports analysis implementation %s", dependency)
		}
	}
}

func TestAnalyzerHostDoesNotImportTraversal(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/analyzerhost").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/workflow", "actionlint.kjanat.dev/v2/internal/workspace", "actionlint.kjanat.dev/v2/internal/config":
			t.Errorf("analyzer scheduling imports traversal or input selection %s", dependency)
		}
	}
}

func TestSemanticChecksAndYAMLBoundary(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/semantics/...").Output()
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
			if strings.HasPrefix(dependency, "go.yaml.in/") || strings.HasPrefix(dependency, "gopkg.in/yaml.") {
				t.Errorf("semantic check %s depends on YAML decoding %s", pkg.ImportPath, dependency)
			}
			for _, domain := range []string{"workflow", "action", "builtin", "engine", "config", "workspace", "analyzerhost", "tools", "render"} {
				prefix := "actionlint.kjanat.dev/v2/internal/" + domain
				if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
					t.Errorf("semantic check %s depends on adapter or host %s", pkg.ImportPath, dependency)
				}
			}
		}
	}
}

func TestYAMLParserIsConfinedToCodec(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/internal/yaml/...", "actionlint.kjanat.dev/v2/internal/source/text").Output()
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
		if pkg.ImportPath == "actionlint.kjanat.dev/v2/internal/yaml/codec" {
			continue
		}
		for _, dependency := range pkg.Deps {
			if strings.HasPrefix(dependency, "go.yaml.in/") || strings.HasPrefix(dependency, "gopkg.in/yaml.") || dependency == "actionlint.kjanat.dev/v2/internal/yaml/codec" {
				t.Errorf("YAML model or source mapping %s imports codec dependency %s", pkg.ImportPath, dependency)
			}
		}
	}
}

func TestNewContractsDoNotImportTheirImplementations(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-json", "actionlint.kjanat.dev/v2/rule", "actionlint.kjanat.dev/v2/source", "actionlint.kjanat.dev/v2/resolver", "actionlint.kjanat.dev/v2/internal/langserver", "actionlint.kjanat.dev/v2/internal/fix", "actionlint.kjanat.dev/v2/internal/rulecatalog").Output()
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
			case "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/cli", "actionlint.kjanat.dev/v2/internal/localapp", "actionlint.kjanat.dev/v2/internal/plugins/component", "actionlint.kjanat.dev/v2/internal/config":
				t.Errorf("%s imports host implementation %s", pkg.ImportPath, dependency)
			}
		}
	}
}

// Configuration schemas are generated from their owning model and catalogue;
// loading an analysis engine to discover configuration metadata breaks that boundary.
func TestConfigurationGeneratorDoesNotImportEngine(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/tools/config").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if dependency == "actionlint.kjanat.dev/v2/internal/engine" || dependency == "actionlint.kjanat.dev/v2/internal/cli" {
			t.Errorf("configuration generator imports host implementation %s", dependency)
		}
	}
}

// The result model and semantic execution must remain independent of presentation.
// The test checks transitive dependencies so a facade cannot hide a reversed edge.
func TestAnalysisDoesNotDependOnPresentation(t *testing.T) {
	for _, name := range []string{"internal/report", "internal/engine", "internal/application", "analysis"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if dependency == "actionlint.kjanat.dev/v2/internal/render" || dependency == "github.com/fatih/color" || dependency == "github.com/spf13/cobra" {
				t.Errorf("%s depends on presentation through %s", name, dependency)
			}
		}
	}
}

func TestLanguagePreparationDoesNotDependOnExecution(t *testing.T) {
	for _, name := range []string{"internal/workflow/facts", "internal/workflow/checks", "internal/workflow/schema", "internal/workflow/references"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			switch dependency {
			case "os/exec", "actionlint.kjanat.dev/v2/internal/analyzerhost", "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/config":
				t.Errorf("%s depends on execution/configuration through %s", name, dependency)
			}
		}
	}
}

// Dependency metadata may read repository sources, but must not construct a
// rule host, launch a checker, or render diagnostics to parse those sources.
func TestWorkflowDependencyMetadataDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/workflow/calls").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		switch dependency {
		case "actionlint.kjanat.dev/v2/internal/engine", "actionlint.kjanat.dev/v2/internal/application", "actionlint.kjanat.dev/v2/internal/analyzerhost", "actionlint.kjanat.dev/v2/internal/render", "actionlint.kjanat.dev/v2/internal/tools/adapters", "actionlint.kjanat.dev/v2/internal/config", "actionlint.kjanat.dev/v2/internal/workspace":
			t.Errorf("workflow dependency metadata imports host %s", dependency)
		}
	}
}

func TestToolIntegrationDependencyDirection(t *testing.T) {
	for _, name := range []string{"engine", "application", "tools", "tools/adapters", "tools/backend", "tools/ruff", "tools/shellcheck", "tools/selection"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			if name == "engine" || name == "application" {
				if dependency == "actionlint.kjanat.dev/v2/internal/tools/adapters" || dependency == "actionlint.kjanat.dev/v2/internal/tools/ruff" || dependency == "actionlint.kjanat.dev/v2/internal/tools/backend" || dependency == "actionlint.kjanat.dev/v2/internal/tools/shellcheck" || dependency == "actionlint.kjanat.dev/v2/internal/tools/selection" {
					t.Errorf("%s depends on tool implementation %s", name, dependency)
				}
			} else if dependency == "actionlint.kjanat.dev/v2/internal/engine" || dependency == "actionlint.kjanat.dev/v2/internal/application" {
				t.Errorf("%s depends on host %s", name, dependency)
			}
		}
	}
}

// Observations may be consumed by more than one language adapter. They must not
// acquire semantic policy through a dependency on the expression or workflow code.
func TestWebhookObservationsHaveNoSemanticDependencies(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/webhook/payload").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		if strings.HasPrefix(dependency, "actionlint.kjanat.dev/v2/") {
			t.Errorf("webhook observations import semantic dependency %s", dependency)
		}
	}
}

// Extracted language and suppression domains return values to the host; neither
// direct imports nor aliases may pull orchestration, policy loading or tools back in.
func TestExtractedDomainsDoNotImportHost(t *testing.T) {
	packages := []string{
		"actionlint.kjanat.dev/v2/internal/action",
		"actionlint.kjanat.dev/v2/internal/action/validation",
		"actionlint.kjanat.dev/v2/internal/action/local",
		"actionlint.kjanat.dev/v2/internal/fsread",
		"actionlint.kjanat.dev/v2/internal/suppression",
		"actionlint.kjanat.dev/v2/internal/workflow/matrix",
		"actionlint.kjanat.dev/v2/internal/workflow/cachepolicy",
		"actionlint.kjanat.dev/v2/internal/workflow/names",
		"actionlint.kjanat.dev/v2/internal/actioncatalog",
		"actionlint.kjanat.dev/v2/internal/workflow/runner",
		"actionlint.kjanat.dev/v2/internal/workflow/execution",
		"actionlint.kjanat.dev/v2/internal/workflow/expressions",
		"actionlint.kjanat.dev/v2/internal/workflow/visit",
		"actionlint.kjanat.dev/v2/internal/finding",
		"actionlint.kjanat.dev/v2/internal/actioncatalog/metadata",
		"actionlint.kjanat.dev/v2/internal/actioncatalog/generate",
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
				prefix := "actionlint.kjanat.dev/v2/" + forbidden
				if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
					t.Errorf("domain %s imports host implementation %s", pkg.ImportPath, dependency)
				}
			}
		}
	}
}

func TestDiagnosticPolicyDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/diagnosticpolicy").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, forbidden := range []string{"engine", "application", "cli", "workspace", "analyzerhost", "tools", "render"} {
			prefix := "actionlint.kjanat.dev/v2/internal/" + forbidden
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("diagnostic policy imports host %s", dependency)
			}
		}
	}
}

// Built-in registration owns concrete adapters; it receives observations through
// callbacks and cannot construct application sessions or execution transports.
func TestBuiltinCompositionDoesNotImportHost(t *testing.T) {
	output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/builtin").Output()
	if err != nil {
		t.Fatal(err)
	}
	for dependency := range strings.SplitSeq(string(output), "\n") {
		for _, forbidden := range []string{"engine", "application", "cli", "workspace", "analyzerhost", "tools", "render"} {
			prefix := "actionlint.kjanat.dev/v2/internal/" + forbidden
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Errorf("built-in composition imports host implementation %s", dependency)
			}
		}
		if dependency == "os/exec" {
			t.Error("built-in composition launches processes")
		}
	}
}

// Bundling must not turn the two native integrations into a shared implementation.
func TestBundledToolsHaveIndependentImplementations(t *testing.T) {
	for _, name := range []string{"ruff", "shellcheck"} {
		output, err := exec.CommandContext(t.Context(), "go", "list", "-f", `{{join .Deps "\n"}}`, "actionlint.kjanat.dev/v2/internal/tools/"+name).Output()
		if err != nil {
			t.Fatal(err)
		}
		for dependency := range strings.SplitSeq(string(output), "\n") {
			prefix := "actionlint.kjanat.dev/v2/internal/tools/"
			if strings.HasPrefix(dependency, prefix) && dependency != prefix+"selection" {
				t.Errorf("tool %s imports another execution implementation %s", name, dependency)
			}
		}
	}
}
