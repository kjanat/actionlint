package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

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

func TestReportCannotReplaceConsumedFile(t *testing.T) {
	for _, modern := range []bool{false, true} {
		for _, kind := range []string{"stdin", "action", "reusable", "hardlink", "symlink"} {
			t.Run(fmtTestName(modern, kind), func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				for _, dir := range []string{".git", ".github/workflows", "local"} {
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
				}
				target := ".github/workflows/input.yml"
				workflow := commandGoodWorkflow
				content := workflow
				switch kind {
				case "action", "hardlink", "symlink":
					target = "local/action.yml"
					content = "name: local\nruns:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n"
					workflow = "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: ./local\n"
				case "reusable":
					target = ".github/workflows/called.yml"
					content = strings.Replace(commandGoodWorkflow, "on: push", "on: workflow_call", 1)
					workflow = "on: push\njobs:\n  test:\n    uses: ./.github/workflows/called.yml\n"
				}
				if err := os.WriteFile(target, []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
				report := target
				if kind == "hardlink" || kind == "symlink" {
					report = "alias.yml"
					var err error
					if kind == "hardlink" {
						err = os.Link(target, report)
					} else {
						err = os.Symlink(filepath.Join(root, target), report)
					}
					if err != nil {
						t.Skip(err)
					}
				}
				args := []string{"--output-file", report, "--stdin-filename", target, "-"}
				input := workflow
				if kind != "stdin" {
					if err := os.WriteFile(".github/workflows/input.yml", []byte(workflow), 0644); err != nil {
						t.Fatal(err)
					}
					args = []string{"--output-file", report, ".github/workflows/input.yml"}
					input = ""
				}
				if modern {
					args = append([]string{"check"}, args...)
				}
				got := testRunCommand(input, args...)
				if got.Status != 3 || !strings.Contains(got.Stderr, "also an input") {
					t.Fatalf("%+v", got)
				}
				after, err := os.ReadFile(target)
				if err != nil || string(after) != content {
					t.Fatalf("input changed: %q, %v", after, err)
				}
			})
		}
	}
}
func fmtTestName(modern bool, kind string) string {
	if modern {
		return "modern/" + kind
	}
	return "legacy/" + kind
}

func TestReportPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("report", []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}
	got := testRunCommand(commandGoodWorkflow, "check", "--output-file", "report", "-")
	if got.Status != 0 {
		t.Fatal(got)
	}
	info, err := os.Stat("report")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatal(info.Mode())
	}
}

func TestUnknownOperation(t *testing.T) {
	_, err := executeInvocation(context.Background(), Command{}, invocation{Operation: "typo"})
	if err == nil {
		t.Fatal("unknown operation executed")
	}
}
func TestCompletionProtocolWithFilename(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("__complete", []byte(commandBadWorkflow), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	command := Command{Stdout: &out, Stderr: &stderr, Stdin: strings.NewReader("")}
	if code := command.Main([]string{"actionlint", "__complete", "check", "--output-format", ""}); code != 0 || !strings.Contains(out.String(), "json") {
		t.Fatalf("%d: %s %s", code, &out, &stderr)
	}
	got := testRunCommand("", "--", "__complete")
	if got.Status != 1 || !strings.Contains(got.Stdout, "[expression]") {
		t.Fatal(got)
	}
}
func TestManualNativeJSONContract(t *testing.T) {
	data, err := os.ReadFile("man/actionlint.1.md")
	if err != nil {
		t.Fatal(err)
	}
	result := testRunCommand(commandGoodWorkflow, "check", "--json", "-")
	if result.Status != 0 || !strings.Contains(string(data), strings.TrimSpace(result.Stdout)) {
		t.Fatalf("manual lacks clean JSON example %s", result.Stdout)
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
