package githubaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"actionlint.kjanat.dev"
)

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
		name, link, content, caller, readError string
		directory                              bool
	}{
		{"action yaml", ".github/actions/local/action.yaml", metadata, actionWorkflow, "could not read action metadata", false},
		{"action yml", ".github/actions/local/action.yml", metadata, actionWorkflow, "could not read action metadata", false},
		{"action directory", ".github/actions/local", metadata, actionWorkflow, "could not read action metadata", true},
		{"reusable workflow", ".github/workflows/called.yml", strings.Replace(cleanWorkflow, "on: push", "on: workflow_call", 1), reusableWorkflow, "could not read reusable workflow file", false},
	} {
		for _, contained := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/outside", true: "/inside"}[contained], func(t *testing.T) {
				workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/ci.yml": tc.caller})
				targetDir := t.TempDir()
				content := "read-confinement-marker: [\n"
				if contained {
					targetDir = filepath.Join(workspace, "fixtures")
					content = tc.content
				}
				if err := os.MkdirAll(targetDir, 0o755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(targetDir, "action.yml")
				if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.directory {
					target = targetDir
				}
				linkWorkspaceFile(t, target, filepath.Join(workspace, filepath.FromSlash(tc.link)))
				got := runLinter(&lintRequest{workingDir: workspace, files: []string{".github/workflows/ci.yml"}, format: formatJSON})
				if contained {
					if got.code != actionlint.ExitStatusSuccessNoProblem {
						t.Fatalf("contained dependency failed: %s%s", got.stderr, got.stdout)
					}
					return
				}
				if got.code != actionlint.ExitStatusSuccessProblemFound || !strings.Contains(got.stdout, tc.readError) {
					t.Fatalf("wanted dependency read rejection, got %d: %s%s", got.code, got.stderr, got.stdout)
				}
				if strings.Contains(got.stdout+got.stderr+got.sarif, "read-confinement-marker") {
					t.Fatal("outside dependency contents reached diagnostics")
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
				content := "read-confinement-marker: true\n"
				if contained {
					targetDir = workspace
					content = "self-hosted-runner: {labels: [unknown-runner]}\ntools: {shellcheck: false}\n"
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
				if contained {
					if got.code != actionlint.ExitStatusSuccessNoProblem || code != actionlint.ExitStatusSuccessNoProblem {
						t.Fatalf("contained config failed: lint=%d %s%s, preflight=%d %s", got.code, got.stderr, got.stdout, code, stderr.String())
					}
					if !strings.Contains(stdout.String(), `"shellcheck":false,"pyflakes":false`) {
						t.Fatalf("preflight ignored contained config: %s", stdout.String())
					}
					return
				}
				if got.code != actionlint.ExitStatusFailure || code != actionlint.ExitStatusFailure || !strings.Contains(got.stderr, "could not read config file") || !strings.Contains(stderr.String(), "could not read config file") {
					t.Fatalf("wanted config read rejection: lint=%d %s%s, preflight=%d %s", got.code, got.stderr, got.stdout, code, stderr.String())
				}
				if got.stdout != "" || stdout.Len() != 0 || strings.Contains(got.stderr+stderr.String(), "read-confinement-marker") {
					t.Fatal("outside config contents reached diagnostics or tool selection")
				}
			})
		}
	}
}
