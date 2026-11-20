package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/results"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCommandSeverityExitStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "workflow.yml"), []byte("on: push\npermissions: {}\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ''\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, level := range []string{"info", "warn", "error"} {
		if err := os.WriteFile("actionlint.toml", []byte("[lint.rules.suspicious]\nempty-run = \""+level+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, strict := range []bool{false, true} {
			args := []string{"check", "--offline", "--no-tools", "--config", "actionlint.toml", "--json", "workflow.yml"}
			if strict {
				args = append(args, "--strict")
			}
			got := testRunCommand("", args...)
			var result report.CheckResult
			if err := json.Unmarshal([]byte(got.Stdout), &result); err != nil {
				t.Fatalf("decode result: %v; output=%+v", err, got)
			}
			want := 0
			if level == "error" || strict && level == "warn" {
				want = 1
			}
			if got.Status != want || result.ExitCode != want || len(result.Diagnostics) != 1 || result.Diagnostics[0].Origin != results.ProjectOrigin {
				t.Fatalf("level=%s strict=%t: want exit=%d and one project finding; got %+v, result=%+v", level, strict, want, got, result)
			}
		}
	}
}
