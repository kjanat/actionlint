package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginSuppressionsUseInvocationRegistry(t *testing.T) {
	workflow := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: sudo make install # actionlint:ignore no-sudo -- intentional\n"
	args := []string{"check", "--no-config", "--plugin", pluginFixture("no-sudo"), "-"}
	got := testRunCommand(workflow, args...)
	if got.Status != 0 || got.Stdout != "" {
		t.Fatalf("inline: %+v", got)
	}
	// Registrations belong to one invocation, including back-to-back CLI calls.
	got = testRunCommand(workflow, "check", "--no-config", "-")
	if got.Status != 1 || !strings.Contains(got.Stdout, "unknown inline suppression rule") {
		t.Fatalf("leaked registry: %+v", got)
	}
	path := filepath.Join(t.TempDir(), "actionlint.yaml")
	for _, tc := range []struct {
		config string
		want   int
	}{
		{"ignores: [{rule: no-sudo, file: '**'}]\n", 0},
		{"policy: {disallow-suppressions: {rules: [no-sudo], report: all}}\n", 1},
	} {
		if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
			t.Fatal(err)
		}
		got = testRunCommand(workflow, "check", "--config", path, "--plugin", pluginFixture("no-sudo"), "-")
		if got.Status != tc.want || strings.Contains(got.Stdout, "unknown inline suppression") {
			t.Fatalf("config %s: %+v", tc.config, got)
		}
		if tc.want == 1 && !strings.Contains(got.Stdout, "[disallow-suppressions]") {
			t.Fatalf("policy ignored: %+v", got)
		}
	}
}

const pluginWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -sSf https://example.com/install.sh | sudo bash\n      - run: wget --no-check-certificate https://example.com/x.sh\n"

var componentTestdata = func() string {
	dir, err := filepath.Abs(filepath.Join("..", "plugins", "component", "testdata"))
	if err != nil {
		panic(err)
	}
	return dir
}()

func pluginFixture(name string) string {
	return filepath.Join(componentTestdata, "plugins", name+".wasm")
}

func TestCheckLoadsComponentPlugins(t *testing.T) {
	got := testRunCommand(pluginWorkflow, "check", "--no-config", "--plugin", pluginFixture("pipe-to-shell"), "--plugin", pluginFixture("no-sudo"), "--plugin", pluginFixture("insecure-download"), "-")
	if got.Status != 1 || got.Stderr != "" {
		t.Fatalf("status=%d stderr=%q stdout=%q", got.Status, got.Stderr, got.Stdout)
	}
	for _, want := range []string{"<stdin>:6:57: step runs sudo", "[no-sudo]", "<stdin>:6:62: script pipes a download into bash", "[pipe-to-shell]", "<stdin>:7:19: step disables TLS verification with --no-check-certificate for a download", "[insecure-download]"} {
		if !strings.Contains(got.Stdout, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, got.Stdout)
		}
	}
	clean := testRunCommand("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hello\n", "check", "--no-config", "--plugin", pluginFixture("pipe-to-shell"), "-")
	if clean.Status != 0 || clean.Stdout != "" || clean.Stderr != "" {
		t.Fatalf("clean: %+v", clean)
	}
}

func TestCheckRendersPluginRulesInSARIF(t *testing.T) {
	got := testRunCommand(pluginWorkflow, "check", "--no-config", "--output-format", "sarif", "--plugin", pluginFixture("pipe-to-shell"), "-")
	if got.Status != 1 {
		t.Fatalf("status=%d stderr=%q", got.Status, got.Stderr)
	}
	var log struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID string `json:"ruleId"`
				Level  string `json:"level"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &log); err != nil {
		t.Fatal(err)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 || log.Runs[0].Results[0].RuleID != "pipe-to-shell" || log.Runs[0].Results[0].Level != "warning" {
		t.Fatalf("results=%+v", log.Runs)
	}
	listed := false
	for _, rule := range log.Runs[0].Tool.Driver.Rules {
		listed = listed || rule.ID == "pipe-to-shell"
	}
	if !listed {
		t.Fatalf("rules=%+v", log.Runs[0].Tool.Driver.Rules)
	}
}

func TestCheckReportsPluginFailures(t *testing.T) {
	missing := testRunCommand(pluginWorkflow, "check", "--no-config", "--plugin", pluginFixture("missing"), "-")
	if missing.Status != 2 || !strings.Contains(missing.Stderr, "missing.wasm") {
		t.Fatalf("missing: %+v", missing)
	}
	refused := testRunCommand(pluginWorkflow, "check", "--no-config", "--plugin", filepath.Join(componentTestdata, "wat", "forbidden-import.wasm"), "-")
	if refused.Status != 2 || !strings.Contains(refused.Stderr, "plugin imports wasi:sockets/tcp-create-socket@0.2.9, which this host does not grant") {
		t.Fatalf("refused: %+v", refused)
	}
	failing := testRunCommand(pluginWorkflow, "check", "--no-config", "--plugin", pluginFixture("misbehave"), "--log-level", "debug", "-")
	if failing.Status != 3 || !strings.Contains(failing.Stderr, `analyzer "misbehave": plugin misbehave failed (memory 64 MiB, time 10s)`) {
		t.Fatalf("failing: %+v", failing)
	}
	if !strings.Contains(failing.Stderr, `[Linter] Loaded plugin "misbehave" from `) || !strings.Contains(failing.Stderr, "[plugin misbehave.wasm] ") {
		t.Fatalf("debug log: %q", failing.Stderr)
	}
}
