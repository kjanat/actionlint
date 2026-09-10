package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestModernConfigCreationAndYAMLOrigins(t *testing.T) {
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
	if got := testRunCommand("", "config", "show"); got.Status != 0 || !strings.Contains(got.Stdout, "config-variables: null") {
		t.Fatal(got)
	}
	config := "defaults: &defaults {config-variables: []}\n<<: *defaults\nconfig-secrets: null\n"
	if err := os.WriteFile(result["path"], []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--origin", "--json")
	var inspection actionlint.ConfigInspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	if inspection.Origins["/config-variables"].Source != "config" || inspection.Origins["/config-secrets"].State != "null" {
		t.Fatal(inspection.Origins)
	}
	if _, exists := inspection.Origins["/defaults"]; exists {
		t.Fatal("ignored setting reported as effective")
	}
}

func TestModernConfigSelectionAndOrigins(t *testing.T) {
	commandTestRepo(t)
	config := "config-variables: []\nconfig-secrets: null\npolicy:\n  require-commit-hash: false\n  required-actions: []\n  require-job-timeout: {min-minutes: 2, max-minutes: 10}\n"
	if err := os.WriteFile(".github/actionlint.yml", []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand("", "config", "show", "--json", "--origin")
	var inspection actionlint.ConfigInspection
	if err := json.Unmarshal([]byte(got.Stdout), &inspection); err != nil || got.Status != 0 {
		t.Fatalf("%+v (%v)", got, err)
	}
	for key, state := range map[string]string{"/config-variables": "value", "/config-secrets": "null", "/policy/require-commit-hash": "value", "/policy/required-actions": "value"} {
		origin := inspection.Origins[key]
		if origin.Source != "config" || origin.State != state || origin.Line == 0 {
			t.Errorf("%s: %+v", key, origin)
		}
	}
	if inspection.Origins["/assume-default-permissions"].Source != "default" {
		t.Fatal(inspection.Origins)
	}
	if inspection.Config["config-variables"] == nil || inspection.Config["config-secrets"] != nil {
		t.Fatal("[] and null lost their distinction")
	}
	policy := inspection.Config["policy"].(map[string]any)
	if policy["require-commit-hash"] != false || policy["required-actions"] == nil {
		t.Fatal(policy)
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
	t.Chdir(t.TempDir())
	for _, dir := range []string{".git", ".github/workflows"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(".github/workflows/ci.yml", []byte(commandGoodWorkflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"actionlint"}} {
		var out, errout bytes.Buffer
		cmd := Command{Stdout: &out, Stderr: &errout}
		if code := cmd.Main(args); code != 0 || out.Len() != 0 || errout.Len() != 0 {
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
	if err := os.WriteFile(created.Path, []byte("paths:\n  '**':\n    ignore: ['undefined variable']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = testRunCommand(commandBadWorkflow, "--config-file", created.Path, "--json", "-")
	if got.Status != 0 || got.Stdout != "{\"schema_version\":1,\"diagnostics\":[]}\n" || got.Stderr != "" {
		t.Fatalf("explicit config not applied: %+v", got)
	}
}

func TestConfigOriginNestedReplacement(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, replacement := range []string{"{}", "null", "{required-actions: [actions/checkout]}"} {
		source := "defaults: &defaults\n  policy:\n    require-commit-hash: true\n<<: *defaults\npolicy: " + replacement + "\n"
		if err := os.WriteFile("config.yml", []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
		result, err := actionlint.InspectConfig(actionlint.ConfigSelection{Path: "config.yml"}, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.Origins["/policy/require-commit-hash"].Source != "default" {
			t.Fatalf("stale origin for %s: %+v", replacement, result)
		}
		if result.Config["policy"].(map[string]any)["require-commit-hash"] != false {
			t.Fatal(result.Config)
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
