package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const pluginWorkflow = "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl -sSf https://example.com/install.sh | sudo bash\n      - run: wget --no-check-certificate https://example.com/x.sh\n"

var componentTestdata = func() string {
	dir, err := filepath.Abs(filepath.Join("..", "component", "testdata"))
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
