package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	resultreport "actionlint.kjanat.dev/v2/internal/results/report"
)

func TestCollectionComparisonSeverity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, settings, severity string
		strict                   bool
	}{
		{"recommended", "", "error", false},
		{"on", "lint: {rules: {preset: none, suspicious: {collection-comparison: on}}}\n", "error", false},
		{"warn", "lint: {rules: {preset: none, suspicious: {collection-comparison: warn}}}\n", "warning", false},
		{"off", "lint: {rules: {suspicious: {collection-comparison: off}}}\n", "", false},
		{"strict", "", "error", true},
	} {
		for _, kind := range []string{"workflow", "composite"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				config := filepath.Join(root, "actionlint.yaml")
				if err := os.WriteFile(config, []byte("plugins: []\n"+tc.settings), 0o600); err != nil {
					t.Fatal(err)
				}
				const expression = `fromJSON('{}') != fromJSON('{}')`
				name := "workflow.yml"
				source := "name: Collection comparison\non: push\npermissions: read-all\njobs:\n  check:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    permissions: read-all\n    steps:\n      - run: echo checked\n        if: ${{ " + expression + " }}\n"
				if kind == "composite" {
					name = "action.yml"
					source = "name: Collection comparison\ndescription: Check collection identity\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo checked\n      if: ${{ " + expression + " }}\n"
				}
				path := filepath.Join(root, name)
				if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"actionlint", "check", "--config", config, "--output-format=json", "--summary=false", path}
				if tc.strict {
					args = append(args, "--strict")
				}
				var out, log bytes.Buffer
				command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
				status := command.MainContext(t.Context(), args)
				if status > 1 {
					t.Fatalf("status=%d stderr=%s output=%s", status, log.String(), out.String())
				}
				var report resultreport.CheckResult
				if err := json.Unmarshal(out.Bytes(), &report); err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, finding := range report.Diagnostics {
					if finding.Rule != "collection-comparison" {
						continue
					}
					found++
					if finding.Severity != tc.severity {
						t.Errorf("severity=%s, want %s", finding.Severity, tc.severity)
					}
					line := []rune(strings.Split(source, "\n")[finding.Start.Line-1])
					if finding.Start.Line != finding.End.Line || string(line[finding.Start.Column-1:finding.End.Column-1]) != expression {
						t.Errorf("incorrect range: %+v", finding)
					}
				}
				want := 1
				if tc.severity == "" {
					want = 0
				}
				if found != want {
					t.Fatalf("found=%d, want %d; diagnostics=%+v", found, want, report.Diagnostics)
				}
				if tc.severity == "error" && status != 1 {
					t.Errorf("error finding exited %d, want 1", status)
				}
			})
		}
	}
}

func TestCollectionComparisonAcceptsAutofixWorkflow(t *testing.T) {
	t.Parallel()
	// actionlint f03a587da066c821a400d81e5990662b36754dae uses null checks
	// to choose how fixes are published for stacked pull requests.
	const path = ".github/workflows/autofix.yml"
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, condition := range []string{"github.event.pull_request.stack != null", "github.event.pull_request.stack == null"} {
		if !bytes.Contains(source, []byte(condition)) {
			t.Fatalf("workflow no longer exercises %s", condition)
		}
	}
	config := filepath.Join(t.TempDir(), "actionlint.yaml")
	if err := os.WriteFile(config, []byte("plugins: []\nlint: {rules: {preset: none, suspicious: {collection-comparison: on}}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
	status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", config, "--offline", "--output-format=json", "--summary=false", path})
	if status != 0 {
		t.Fatalf("status=%d stderr=%s output=%s", status, log.String(), out.String())
	}
	var report resultreport.CheckResult
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 0 {
		t.Fatalf("valid null checks reported: %+v", report.Diagnostics)
	}
}

func TestCollectionComparisonWriteGate(t *testing.T) {
	t.Parallel()
	const source = "name: Write gate\non: issues\npermissions:\n  issues: write\njobs:\n  update:\n    runs-on: ubuntu-latest\n    steps:\n      - if: ${{ github.event.issue != fromJSON('{}') }}\n        run: gh api --method PATCH repos/example/project/issues/1 -f title=approved\n"
	root := t.TempDir()
	config := filepath.Join(root, "actionlint.yaml")
	path := filepath.Join(root, "workflow.yml")
	if err := os.WriteFile(config, []byte("plugins: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, log bytes.Buffer
	command := Command{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &log}
	status := command.MainContext(t.Context(), []string{"actionlint", "check", "--config", config, "--output-format=json", "--summary=false", path})
	if status != 1 {
		t.Fatalf("status=%d stderr=%s output=%s", status, log.String(), out.String())
	}
	var report resultreport.CheckResult
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Rule != "collection-comparison" || report.Diagnostics[0].Severity != "error" {
		t.Fatalf("unsafe write gate did not emit an error: %+v", report.Diagnostics)
	}
}
