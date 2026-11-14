package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func configuredOutputFixture(t *testing.T, settings string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "actionlint.yaml")
	if err := os.WriteFile(config, []byte("plugins: []\n"+settings), 0600); err != nil {
		t.Fatal(err)
	}
	workflow := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(workflow, []byte("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ''\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return config, workflow
}

func runConfiguredOutput(t *testing.T, config, workflow string, flags ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := Command{Stdout: &stdout, Stderr: &stderr}
	args := []string{"actionlint", "check", "--config", config, "--no-tool=shellcheck", "--no-tool=ruff", "--offline"}
	args = append(args, flags...)
	args = append(args, workflow)
	status := command.MainContext(t.Context(), args)
	return status, stdout.String(), stderr.String()
}

func TestConfiguredOutputEmitsEachFormatOnce(t *testing.T) {
	t.Parallel()
	config, workflow := configuredOutputFixture(t, "output: {formats: [github, pretty, github], log-level: info, summary: true, network-stats: true}\n")
	status, out, log := runConfiguredOutput(t, config, workflow)
	if status != 1 || strings.Count(out, "::warning ") != 1 || strings.Count(out, "warning[empty-run]") != 1 {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
	if !strings.Contains(log, "verbose: actionlint ") || !strings.Contains(log, "Checked 1 file") || !strings.Contains(log, `"network":`) {
		t.Fatalf("missing configured reporting: %s", log)
	}
}

func TestConfiguredOutputExplicitFlagsOverride(t *testing.T) {
	t.Parallel()
	config, workflow := configuredOutputFixture(t, "output: {formats: [github, pretty], log-level: debug, summary: true, network-stats: true, quiet: true}\n")
	status, out, log := runConfiguredOutput(t, config, workflow, "--json", "--log-level=none", "--summary=false", "--network-stats=false", "--quiet=false")
	if status != 1 || !json.Valid([]byte(out)) || log != "" {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
	status, out, log = runConfiguredOutput(t, config, workflow, "--output=gcc", "--output=gcc", "--verbose=false", "--summary=false", "--network-stats=false")
	if status != 1 || strings.Contains(out, "::warning") || strings.Contains(out, "--> ") || strings.Count(out, "empty-run") != 1 || log != "" {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
}

func TestConfiguredOutputEnvironmentOverrides(t *testing.T) {
	config, workflow := configuredOutputFixture(t, "output: {formats: [github], log-level: debug, summary: true}\n")
	t.Setenv("ACTIONLINT_OUTPUT_FORMAT", "json")
	t.Setenv("ACTIONLINT_LOG_LEVEL", "none")
	t.Setenv("ACTIONLINT_SUMMARY", "false")
	status, out, log := runConfiguredOutput(t, config, workflow)
	if status != 1 || !json.Valid([]byte(out)) || log != "" {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
	status, out, _ = runConfiguredOutput(t, config, workflow, "-o", "github")
	if status != 1 || !strings.Contains(out, "::warning ") {
		t.Fatalf("CLI did not override environment: %s", out)
	}
}

func TestConfiguredOutputNoConfigAndStdin(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "actionlint.yaml"), []byte("plugins: []\noutput: {formats: [github], summary: false}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, disabled := range []bool{false, true} {
		var out, log bytes.Buffer
		command := Command{Stdin: strings.NewReader("on: push\npermissions: {}\njobs: {}\n"), Stdout: &out, Stderr: &log}
		args := []string{"actionlint", "check", "--no-tool=shellcheck", "--no-tool=ruff", "--offline", "-"}
		if disabled {
			args = append(args, "--no-config", "--summary=false")
		}
		status := command.MainContext(t.Context(), args)
		if status != 1 || strings.Contains(out.String(), "::error ") == disabled {
			t.Fatalf("disabled=%t status=%d out=%s log=%s", disabled, status, &out, &log)
		}
	}
}

func TestConfiguredOutputEmptyEnvironmentUsesConfig(t *testing.T) {
	t.Setenv("ACTIONLINT_OUTPUT_FORMAT", "")
	t.Setenv("ACTIONLINT_LOG_LEVEL", "")
	config, workflow := configuredOutputFixture(t, "output: {formats: [github], log-level: info, summary: false}\n")
	status, out, log := runConfiguredOutput(t, config, workflow)
	if status != 1 || !strings.Contains(out, "::warning ") || !strings.Contains(log, "verbose: actionlint ") {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
}

func TestConfiguredOutputInheritsAcrossFormats(t *testing.T) {
	t.Parallel()
	config, workflow := configuredOutputFixture(t, "")
	dir := filepath.Dir(config)
	if err := os.WriteFile(filepath.Join(dir, "base.jsonc"), []byte("{ // shared output\n\"output\": {\"formats\": [\"github\"], \"summary\": true,},\n}"), 0600); err != nil {
		t.Fatal(err)
	}
	config = filepath.Join(dir, "actionlint.toml")
	if err := os.WriteFile(config, []byte("extends = ['base.jsonc']\nplugins = []\n[output]\nsummary = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status, out, log := runConfiguredOutput(t, config, workflow)
	if status != 1 || strings.Count(out, "::warning ") != 1 || log != "" {
		t.Fatalf("status=%d out=%s log=%s", status, out, log)
	}
}

func TestConfiguredOutputUsesInvokingRepository(t *testing.T) {
	root := t.TempDir()
	makeRepo := func(name, output string) string {
		t.Helper()
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, ".github"), 0700); err != nil {
			t.Fatal(err)
		}
		if result, err := exec.CommandContext(t.Context(), "git", "init", "-q", dir).CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, result)
		}
		if err := os.WriteFile(filepath.Join(dir, ".github", "actionlint.yaml"), []byte("plugins: []\noutput: "+output+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "workflow.yml"), []byte("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ''\n"), 0600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	invoker := makeRepo("invoker", "{formats: [gcc], summary: false}")
	first := makeRepo("first", "{formats: [json], log-level: debug, summary: true}")
	second := makeRepo("second", "{formats: [github], network-stats: true}")
	t.Chdir(invoker)
	var out, log bytes.Buffer
	command := Command{Stdout: &out, Stderr: &log}
	status := command.MainContext(t.Context(), []string{"actionlint", "check", "--no-tool=shellcheck", "--no-tool=ruff", "--offline", filepath.Join(first, "workflow.yml"), filepath.Join(second, "workflow.yml")})
	if status != 1 || strings.Count(out.String(), "empty-run") != 2 || strings.Contains(out.String(), "::warning") || log.Len() != 0 {
		t.Fatalf("status=%d out=%s log=%s", status, &out, &log)
	}
}

func TestConfiguredExperimentalAndOverrides(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		for _, flag := range []string{"", "--experimental=true", "--experimental=false"} {
			t.Run(fmt.Sprintf("config=%t/flag=%s", enabled, flag), func(t *testing.T) {
				t.Parallel()
				config, workflow := configuredOutputFixture(t, fmt.Sprintf("lint: {experimental: %t}\noutput: {formats: [json], summary: false}\n", enabled))
				if err := os.WriteFile(workflow, []byte("on: push\npermissions: {}\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - shell: bash\n        run: echo \"::notice:Up to date\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				var flags []string
				if flag != "" {
					flags = append(flags, flag)
				}
				status, out, log := runConfiguredOutput(t, config, workflow, flags...)
				var result struct {
					Diagnostics []struct {
						Rule string `json:"rule"`
					} `json:"diagnostics"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatalf("status=%d out=%s log=%s: %v", status, out, log, err)
				}
				found := false
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Rule == "workflow-command-syntax" {
						found = true
					}
				}
				want := enabled
				if flag != "" {
					want = flag == "--experimental=true"
				}
				if found != want {
					t.Fatalf("found=%t want=%t status=%d out=%s log=%s", found, want, status, out, log)
				}
			})
		}
	}
}
