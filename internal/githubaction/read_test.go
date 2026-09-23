package githubaction

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

func TestActionReadsPathsOutsideWorkspace(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{"ci.yml": brokenWorkflow})
	other := workspaceWith(t, map[string]string{
		"ci.yml":         brokenWorkflow,
		"actionlint.yml": "self-hosted-runner: {labels: [unknown-runner]}\ntools: {shellcheck: false}\n",
	})
	relative, err := filepath.Rel(workspace, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, directory, file, config string }{
		{"relative file and config", ".", filepath.Join(relative, "ci.yml"), filepath.Join(relative, "actionlint.yml")},
		{"absolute file and config", ".", filepath.Join(other, "ci.yml"), filepath.Join(other, "actionlint.yml")},
		{"relative working directory", relative, "ci.yml", "actionlint.yml"},
		{"absolute working directory", other, "ci.yml", "actionlint.yml"},
		{"external config for local file", ".", "ci.yml", filepath.Join(other, "actionlint.yml")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_WORKING-DIRECTORY": tc.directory,
				"INPUT_FILES": tc.file, "INPUT_CONFIG-FILE": tc.config,
				"INPUT_PYFLAKES": "false",
				"INPUT_FORMAT":   "json", "GITHUB_OUTPUT": filepath.Join(t.TempDir(), "outputs"),
				"ACTIONLINT_ACTION_RESULT": filepath.Join(t.TempDir(), "result.json"),
			}
			getenv := func(name string) string { return env[name] }
			var plan, errors, output strings.Builder
			if code := ToolPlan(getenv, &plan, &errors); code != actionlint.ExitStatusSuccessNoProblem {
				t.Fatalf("tool plan failed: %d %s", code, errors.String())
			}
			var tools actionlint.ExternalToolRequirements
			if err := json.Unmarshal([]byte(plan.String()), &tools); err != nil {
				t.Fatal(err)
			}
			if tools.Shellcheck || tools.Pyflakes {
				t.Fatalf("tool plan ignored external config: %s", plan.String())
			}
			if code := Main(getenv, &output); code != actionlint.ExitStatusSuccessNoProblem {
				t.Fatalf("analysis failed: %d %s", code, output.String())
			}
			var result persistedResult
			if err := json.Unmarshal([]byte(read(t, env["ACTIONLINT_ACTION_RESULT"])), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Completed || len(result.Diagnostics) != 0 {
				t.Fatalf("expected completed analysis honoring external config: %+v", result)
			}
		})
	}
}

func linkWorkspaceFile(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Dir(link), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestActionReportsExternalWorkflowFindings(t *testing.T) {
	workspace := workspaceWith(t, nil)
	outside := workspaceWith(t, map[string]string{"workflow.yml": brokenWorkflow})
	file := filepath.Join(outside, "workflow.yml")
	relative, err := filepath.Rel(workspace, file)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"github", "json", "sarif"} {
		t.Run(format, func(t *testing.T) {
			env := map[string]string{
				"GITHUB_WORKSPACE": workspace, "INPUT_FILES": file, "INPUT_FORMAT": format,
				"INPUT_SHELLCHECK": "false", "INPUT_PYFLAKES": "false",
				"ACTIONLINT_ACTION_RESULT": filepath.Join(t.TempDir(), "result.json"),
				"GITHUB_OUTPUT":            filepath.Join(t.TempDir(), "outputs"),
			}
			var output strings.Builder
			if code := Main(func(name string) string { return env[name] }, &output); code != actionlint.ExitStatusSuccessProblemFound {
				t.Fatalf("external workflow findings lost: %d %s", code, output.String())
			}
			var result persistedResult
			if err := json.Unmarshal([]byte(read(t, env["ACTIONLINT_ACTION_RESULT"])), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Completed || len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != relative {
				t.Fatalf("external workflow diagnostic: %+v", result)
			}
			if !strings.Contains(string(result.SARIF), filepath.ToSlash(relative)) {
				t.Fatalf("external path absent from SARIF: %s", result.SARIF)
			}
			if format == "github" && !strings.Contains(output.String(), "::error file="+filepath.ToSlash(relative)+",") {
				t.Fatalf("external path absent from annotation: %s", output.String())
			}
		})
	}
}

func TestRunLinterDependentSymlinks(t *testing.T) {
	const metadata = "name: local\ndescription: local\nruns: {using: composite, steps: [{run: 'echo hi', shell: bash}]}\n"
	const actionWorkflow = `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/local
`
	const reusableWorkflow = `on: push
jobs:
  test:
    uses: ./.github/workflows/called.yml
`
	for _, tc := range []struct {
		name, link, content, caller string
		directory                   bool
	}{
		{"action yaml", ".github/actions/local/action.yaml", metadata, actionWorkflow, false},
		{"action yml", ".github/actions/local/action.yml", metadata, actionWorkflow, false},
		{"action directory", ".github/actions/local", metadata, actionWorkflow, true},
		{"reusable workflow", ".github/workflows/called.yml", strings.Replace(cleanWorkflow, "on: push", "on: workflow_call", 1), reusableWorkflow, false},
	} {
		for _, contained := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/outside", true: "/inside"}[contained], func(t *testing.T) {
				workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/ci.yml": tc.caller})
				targetDir := t.TempDir()
				if contained {
					targetDir = filepath.Join(workspace, "fixtures")
				}
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(targetDir, "action.yml")
				if err := os.WriteFile(target, []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.directory {
					target = targetDir
				}
				linkWorkspaceFile(t, target, filepath.Join(workspace, filepath.FromSlash(tc.link)))
				got := runLinter(&lintRequest{workingDir: workspace, files: []string{".github/workflows/ci.yml"}, format: formatJSON})
				if got.code != actionlint.ExitStatusSuccessNoProblem {
					t.Fatalf("linked dependency failed: %d %s%s", got.code, got.stderr, got.stdout)
				}
			})
		}
	}
}

func TestActionConfigSymlinks(t *testing.T) {
	for _, name := range []string{".github/actionlint.yaml", ".github/actionlint.yml", "custom.yml"} {
		for _, contained := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/outside", true: "/inside"}[contained], func(t *testing.T) {
				workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/ci.yml": brokenWorkflow})
				targetDir := t.TempDir()
				content := "self-hosted-runner: {labels: [unknown-runner]}\ntools: {shellcheck: false}\n"
				if contained {
					targetDir = workspace
				}
				target := filepath.Join(targetDir, "config-target.yml")
				if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				linkWorkspaceFile(t, target, filepath.Join(workspace, filepath.FromSlash(name)))
				req := &lintRequest{workingDir: workspace, format: formatJSON}
				env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_PYFLAKES": "false"}
				if name == "custom.yml" {
					req.configFile = filepath.Join(workspace, name)
					env["INPUT_CONFIG-FILE"] = name
				}
				got := runLinter(req)
				var stdout, stderr strings.Builder
				code := ToolPlan(func(key string) string { return env[key] }, &stdout, &stderr)
				if got.code != actionlint.ExitStatusSuccessNoProblem || code != actionlint.ExitStatusSuccessNoProblem {
					t.Fatalf("linked config failed: lint=%d %s%s, preflight=%d %s", got.code, got.stderr, got.stdout, code, stderr.String())
				}
				if !strings.Contains(stdout.String(), `"shellcheck":false,"pyflakes":false`) {
					t.Fatalf("preflight ignored linked config: %s", stdout.String())
				}
			})
		}
	}
}
