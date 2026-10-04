package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
	"github.com/google/go-cmp/cmp"
)

func TestCheckJSONPartialFindings(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", executable)
	t.Setenv("ACTIONLINT_SHELLCHECK_FLAGS", `["-test.run=^TestCheckPartialAnalyzerHelper$", "--"]`)
	for name, content := range map[string]string{
		"config.yml":     "{}\n",
		"successful.yml": commandBadWorkflow + "        shell: pwsh\n",
		"failing.yml": commandBadWorkflow +
			"        shell: bash\n      - run: FAIL_ANALYZER\n        shell: bash\n",
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, modern := range []bool{false, true} {
		for _, failingFirst := range []bool{false, true} {
			for _, outputFile := range []bool{false, true} {
				t.Run(fmt.Sprintf("modern=%t/failing-first=%t/file=%t", modern, failingFirst, outputFile), func(t *testing.T) {
					args := []string{"actionlint", "--config", "config.yml", "--no-color", "--pyflakes=", "--json"}
					if modern {
						args = append(args, "check")
					}
					if outputFile {
						if err := os.WriteFile("report.json", []byte("previous report"), 0600); err != nil {
							t.Fatal(err)
						}
						args = append(args, "--output-file", "report.json")
					}
					paths := []string{"successful.yml", "failing.yml"}
					if failingFirst {
						slices.Reverse(paths)
					}
					var stdout, stderr bytes.Buffer
					command := Command{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
					status := command.Main(append(args, paths...))
					var result actionlint.CheckResult
					if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
						t.Fatalf("invalid failure envelope: %v: %s", err, &stderr)
					}
					if status != 3 || result.ExitCode != 3 || result.Status != "failure" || result.Completed || result.SchemaVersion != 1 || !strings.Contains(result.Error, "exited with status 2") || stdout.Len() != 0 {
						t.Fatalf("changed failure contract: status=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
					}
					var findings []string
					for _, diagnostic := range result.Diagnostics {
						findings = append(findings, diagnostic.Path+":"+diagnostic.Rule)
					}
					want := []string{"successful.yml:expression", "failing.yml:shellcheck", "failing.yml:expression"}
					if failingFirst {
						want = append(want[1:], want[0])
					}
					if diff := cmp.Diff(want, findings); diff != "" {
						t.Errorf("partial findings (-want +got):\n%s", diff)
					}
					if result.FileCount == nil || *result.FileCount != 2 {
						t.Errorf("selected file count lost: %v", result.FileCount)
					}
					if len(result.Configs) != 1 || filepath.Base(result.Configs[0].File) != "config.yml" {
						t.Errorf("selected configuration lost: %v", result.Configs)
					}
					if outputFile {
						data, err := os.ReadFile("report.json")
						if err != nil || string(data) != "previous report" {
							t.Fatalf("failed analysis replaced report: %q (%v)", data, err)
						}
						artifacts, err := filepath.Glob(".actionlint-report-*")
						if err != nil || len(artifacts) != 0 {
							t.Fatalf("temporary reports remain: %v (%v)", artifacts, err)
						}
					}
				})
			}
		}
	}
}

func TestCheckJSONEmptyPartialFindings(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	t.Setenv("ACTIONLINT_SHELLCHECK_BIN", executable)
	t.Setenv("ACTIONLINT_SHELLCHECK_FLAGS", `["-test.run=^TestCheckPartialAnalyzerHelper$", "--"]`)
	workflow := strings.ReplaceAll(commandGoodWorkflow, "echo ok", "FAIL_ANALYZER")
	if err := os.WriteFile("failing.yml", []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%t", modern), func(t *testing.T) {
			args := []string{"actionlint", "--no-config", "--pyflakes=", "--json"}
			if modern {
				args = append(args, "check")
			}
			var stdout, stderr bytes.Buffer
			command := Command{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}
			status := command.Main(append(args, "failing.yml"))
			var result actionlint.CheckResult
			if err := json.Unmarshal(stderr.Bytes(), &result); err != nil {
				t.Fatalf("invalid failure envelope: %v: %s", err, &stderr)
			}
			if status != 3 || result.ExitCode != 3 || result.Status != "failure" || result.Completed || !strings.Contains(result.Error, "exited with status 2") || stdout.Len() != 0 {
				t.Fatalf("changed failure contract: status=%d, stdout=%s, stderr=%s", status, &stdout, &stderr)
			}
			if result.Diagnostics == nil || len(result.Diagnostics) != 0 {
				t.Fatalf("wanted empty diagnostics array: %s", &stderr)
			}
			if result.FileCount == nil || *result.FileCount != 1 {
				t.Fatalf("selected file count lost: %v", result.FileCount)
			}
		})
	}
}

func TestCheckPartialAnalyzerHelper(t *testing.T) {
	if !slices.Contains(os.Args, "--") {
		return
	}
	script, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "FAIL_ANALYZER") {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, `{"comments":[{"line":2,"endLine":2,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"retained ShellCheck finding"}]}`)
	os.Exit(0)
}
