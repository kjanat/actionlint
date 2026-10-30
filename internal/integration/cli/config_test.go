package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestConfigValidationWarnings(t *testing.T) {
	commandTestRepo(t)
	if err := os.WriteFile(".github/actionlint.yaml", []byte("config-variable: [FOO]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text := testRunCommand("", "config", "validate")
	if text.Status != 0 || !strings.Contains(text.Stdout, "unknown configuration key") || !strings.Contains(text.Stdout, ":1:1:") {
		t.Fatal(text)
	}
	encoded := testRunCommand("", "config", "validate", "--json")
	var result struct {
		Valid    bool
		Warnings []repoconfig.Warning
	}
	if err := json.Unmarshal([]byte(encoded.Stdout), &result); err != nil || !result.Valid || len(result.Warnings) != 1 || encoded.Status != 0 {
		t.Fatalf("%+v: %v", encoded, err)
	}
}

func TestConfigInheritedWarningLocation(t *testing.T) {
	commandTestRepo(t)
	base := filepath.Join(t.TempDir(), "base.yml")
	if err := os.WriteFile(base, []byte(`# inherited settings
config-variable: [TYPO]
`), 0600); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(".github", "actionlint.yaml")
	encodedBase, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("extends: ["+string(encodedBase)+"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "validate")
	if got.Status != 0 || !strings.Contains(got.Stdout, base+":2:1: warning:") {
		t.Fatalf("wrong inherited warning location: %+v", got)
	}
	got = testRunCommand("", "config", "validate", "--json")
	var result struct{ Warnings []repoconfig.Warning }
	if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil || got.Status != 0 || len(result.Warnings) != 1 || result.Warnings[0].File != base {
		t.Fatalf("inherited warning JSON=%+v, %v", got, err)
	}
}

func TestModernConfigCreationAndYAMLOrigins(t *testing.T) {
	wasm, err := os.ReadFile("internal/plugins/component/testdata/plugins/no-sudo.wasm")
	if err != nil {
		t.Fatal(err)
	}
	testutil.NewDefaultPluginRegistryWithArtifact(t, wasm, "shellcheck", "ruff")
	commandTestRepo(t)
	for _, operation := range [][]string{{"-init-config"}, {"config", "init"}, {"version"}, {"rules"}} {
		args := append([]string{"--output-file", ".github/actionlint.yaml"}, operation...)
		if got := testRunCommand("", args...); got.Status != 2 {
			t.Fatalf("accepted check output destination for %q: %+v", operation, got)
		}
		if _, err := os.Stat(".github/actionlint.yaml"); !os.IsNotExist(err) {
			t.Fatalf("invalid output destination created config: %v", err)
		}
	}
	created := testRunCommand("", "config", "init", "--json")
	var result map[string]string
	if err := json.Unmarshal([]byte(created.Stdout), &result); err != nil || created.Status != 0 {
		t.Fatalf("%+v (%v)", created, err)
	}
	data, err := os.ReadFile(result["path"])
	if err != nil || !bytes.Contains(data, []byte("yaml-language-server: $schema=")) {
		t.Fatalf("%s (%v)", data, err)
	}
	if got := testRunCommand("", "config", "show"); got.Status != 0 || !strings.Contains(got.Stdout, "plugins:") {
		t.Fatal(got)
	}
	config := `plugins: []
defaults: &defaults {config-variables: []}
<<: *defaults
config-secrets: null
`
	if err := os.WriteFile(result["path"], []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--origin", "--json")
	var inspection repoconfig.Inspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	if inspection.Origins["/lint/rules/correctness/expression-reference/options/variables"].Source != "config" || inspection.Origins["/lint/rules/correctness/expression-reference/options/secrets"].State != "null" {
		t.Fatal(inspection.Origins)
	}
	if _, exists := inspection.Origins["/defaults"]; exists {
		t.Fatal("ignored setting reported as effective")
	}
}

func TestModernConfigSelectionAndOrigins(t *testing.T) {
	commandTestRepo(t)
	config := `plugins: []
lint:
  rules:
    correctness:
      expression-reference: {level: on, options: {variables: [], secrets: null}}
    policy:
      require-commit-hash: off
      required-actions: {level: on, options: {actions: []}}
      require-job-timeout: {level: on, options: {min-minutes: 2, max-minutes: 10}}
`
	if err := os.WriteFile(".github/actionlint.yml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--json", "--origin")
	var inspection repoconfig.Inspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	for key, state := range map[string]string{"/lint/rules/correctness/expression-reference/options/variables": "value", "/lint/rules/correctness/expression-reference/options/secrets": "null", "/lint/rules/policy/require-commit-hash": "value", "/lint/rules/policy/required-actions/options/actions": "value"} {
		origin := inspection.Origins[key]
		if origin.Source != "config" || origin.State != state || origin.Line == 0 {
			t.Errorf("%s: %+v", key, origin)
		}
	}
	data, err := json.Marshal(inspection.Config)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repoconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ConfigVariables == nil || resolved.ConfigSecrets != nil {
		t.Fatal("[] and null lost their distinction")
	}
	if repoconfig.DiagnosticLevel(resolved, "require-commit-hash") != "off" || resolved.Policy.RequiredActions == nil {
		t.Fatal(resolved.Policy)
	}
	for _, args := range [][]string{{"config", "validate"}, {"config", "path"}, {"config", "show"}} {
		if got := testRunCommand("", args...); got.Status != 0 || got.Stderr != "" || got.Stdout == "" {
			t.Fatal(got)
		}
	}
	if err := os.WriteFile(".github/actionlint.yaml", []byte("policy: {unknown: true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := testRunCommand("", "config", "validate", "--json"); got.Status != 3 || !strings.Contains(got.Stderr, "unknown key") {
		t.Fatal(got)
	}
	if got := testRunCommand("", "config", "path"); got.Status != 0 || !strings.Contains(got.Stdout, "actionlint.yaml") {
		t.Fatal(got)
	}
	if got := testRunCommand("", "config", "init"); got.Status != 3 || !strings.Contains(got.Stderr, "already exists") {
		t.Fatal(got)
	}
	for _, args := range [][]string{
		{"check", "--no-config", "--json"},
		{"check", "--config", ".github/actionlint.yml", "--json"},
	} {
		got := testRunCommand("", args...)
		if got.Status != 1 || got.Stderr != "" || !json.Valid([]byte(got.Stdout)) {
			t.Fatalf("explicit selection loaded invalid repository config: %+v", got)
		}
	}
	conflict := testRunCommand("", "check", "--config", "x.yml", "--no-config")
	if conflict.Status != 2 {
		t.Fatal(conflict)
	}
}

func TestCommandConfigAndEmptyArgs(t *testing.T) {
	testutil.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
	t.Setenv("NO_COLOR", "1")
	t.Chdir(t.TempDir())
	gitRepository(t, ".")
	if err := os.MkdirAll(".github/workflows", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandGoodWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	// A bare invocation shows help. Importing the test binary's own flags
	// instead would end in a usage error.
	for _, args := range [][]string{nil, {"actionlint"}} {
		var out, errout bytes.Buffer
		cmd := cli.Command{Stdout: &out, Stderr: &errout}
		if code := cmd.Main(args); code != 0 || !strings.Contains(out.String(), "Usage:") || errout.Len() != 0 {
			t.Fatalf("empty args imported process args: %d %s %s", code, &out, &errout)
		}
	}
	got := testRunCommand("", "--init-config", "--json")
	var created struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &created); err != nil {
		t.Fatal(err)
	}
	if got.Status != 0 || got.Stderr != "" || filepath.Base(created.Path) != "actionlint.yaml" {
		t.Fatalf("%+v", got)
	}
	data, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "yaml-language-server: $schema=") {
		t.Fatalf("missing editor directive: %s", data)
	}
	refused := testRunCommand("", "--init-config", "--json")
	if refused.Status != 3 || !strings.Contains(refused.Stderr, "already exists") {
		t.Fatalf("%+v", refused)
	}
	if err := os.WriteFile(created.Path, []byte(`plugins: []
paths:
  '**':
    ignore: ['undefined variable']
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand(commandBadWorkflow, "--config-file", created.Path, "--json", "-")
	if got.Status != 0 || !cleanCheckJSON(got.Stdout) || !strings.Contains(got.Stderr, "migrated configuration") {
		t.Fatalf("explicit config not applied: %+v", got)
	}
}

func TestConfigOriginNestedReplacement(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, replacement := range []string{"{}", "null", "{required-actions: {level: on, options: {actions: [actions/checkout]}}}"} {
		source := `defaults: &defaults
  policy:
    require-commit-hash: on
lint:
  rules:
    <<: *defaults
    policy: ` + replacement + "\n"
		if err := os.WriteFile("config.yml", []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := repoconfig.Inspect(repoconfig.Selection{Path: "config.yml"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if origin, exists := result.Origins["/lint/rules/policy/require-commit-hash"]; exists && (origin.File != "" || origin.Line != 0) {
			t.Fatalf("stale origin for %s: %+v", replacement, result)
		}
		data, err := json.Marshal(result.Config)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := repoconfig.Parse(data)
		if err != nil || repoconfig.DiagnosticLevel(resolved, "require-commit-hash") != "off" {
			t.Fatalf("replaced policy survived: %+v %v", result.Config, err)
		}
	}
}

func TestLegacyInitValidatesBeforeWriting(t *testing.T) {
	for _, args := range [][]string{{"-ignore", "["}, {"-format", "{{"}, {"--template-file", "missing.tmpl"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			commandTestRepo(t)
			got := testRunCommand("", append([]string{"-init-config"}, args...)...)
			if got.Status != 3 {
				t.Fatal(got)
			}
			if _, err := os.Stat(".github/actionlint.yaml"); !os.IsNotExist(err) {
				t.Fatal("invalid invocation created a config", err)
			}
		})
	}
}
