package cli

import (
	"encoding/json"
	"os"
	"testing"

	"actionlint.kjanat.dev"
)

func TestCommandActionInputs(t *testing.T) {
	t.Chdir(t.TempDir())
	const manifest = "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - run: echo ${{ secrets.TOKEN }}\n      shell: bash\n"
	for name, content := range map[string]string{"action.yml": manifest, "workflow.yml": commandGoodWorkflow} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, modern := range []bool{false, true} {
		for _, stdin := range []bool{false, true} {
			args := []string{"--json", "--shellcheck=", "--ruff=", "--no-config"}
			if stdin {
				args = append(args, "--stdin-filename=action.yml", "-")
			} else {
				args = append(args, "workflow.yml", "action.yml")
			}
			if modern {
				args = append([]string{"check"}, args...)
			}
			got := testRunCommand(manifest, args...)
			var report actionlint.CheckResult
			if err := json.Unmarshal([]byte(got.Stdout), &report); err != nil {
				t.Fatalf("args=%v: invalid JSON: %v; output=%+v", args, err, got)
			}
			if got.Status != 1 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "action" || report.Diagnostics[0].Path != "action.yml" {
				t.Fatalf("args=%v: expected one metadata error, got %+v, diagnostics=%+v", args, got, report.Diagnostics)
			}
			want := 2
			if stdin {
				want = 1
			}
			if len(report.Documents) != want {
				t.Fatalf("args=%v: want %d documents, got %#v", args, want, report.Documents)
			}
			if _, ok := report.Documents[want-1].(actionlint.ActionOutline); !ok {
				t.Fatalf("args=%v: expected action outline, got %#v", args, report.Documents)
			}
		}
	}
}
