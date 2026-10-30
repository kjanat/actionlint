package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev/v2/internal/cli"
	"actionlint.kjanat.dev/v2/internal/render"
)

const cachePolicySteps = `    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

func TestCommandCachePolicyDefaults(t *testing.T) {
	for _, policy := range []struct{ name, category, workflow string }{
		{"cache-write-untrusted", "security", `on: pull_request_target
cache-mode: write
jobs:
  build:
` + cachePolicySteps},
		{"cache-call-unrestricted", "security", `on: pull_request_target
jobs:
  call:
    uses: example/repo/.github/workflows/build.yaml@main
`},
		{"cache-operation", "correctness", `on: push
cache-mode: none
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/cache@v5
        with: {path: .cache, key: test}
`},
	} {
		for _, config := range []struct {
			name, content string
			disabled      bool
		}{
			{"absent", "", false},
			{"empty category", fmt.Sprintf("lint: {rules: {%s: {}}}\n", policy.category), false},
			{"null category", fmt.Sprintf("lint: {rules: {%s: null}}\n", policy.category), false},
			{"null setting", fmt.Sprintf("lint: {rules: {%s: {%s: null}}}\n", policy.category, policy.name), false},
			{"on setting", fmt.Sprintf("lint: {rules: {%s: {%s: on}}}\n", policy.category, policy.name), false},
			{"off setting", fmt.Sprintf("lint: {rules: {%s: {%s: off}}}\n", policy.category, policy.name), true},
		} {
			for _, explicit := range []bool{false, true} {
				if explicit && config.content == "" {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/explicit=%v", policy.name, config.name, explicit), func(t *testing.T) {
					root := t.TempDir()
					for _, dir := range []string{".git", ".github/workflows"} {
						if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					t.Chdir(root)
					workflow := filepath.Join(root, ".github", "workflows", "cache.yaml")
					if err := os.WriteFile(workflow, []byte(policy.workflow), 0o600); err != nil {
						t.Fatal(err)
					}
					args := []string{"actionlint", "--summary=false", "-no-tool=shellcheck", "-no-tool=ruff", "-format", "{{json .}}"}
					if config.content != "" {
						configPath := filepath.Join(root, ".github", "actionlint.yaml")
						if explicit {
							configPath = filepath.Join(root, "explicit.yaml")
							args = append(args, "-config-file", configPath)
						}
						if err := os.WriteFile(configPath, []byte(config.content), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					args = append(args, workflow)
					var stdout, stderr bytes.Buffer
					cmd := cli.Command{Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr}
					status := cmd.Main(args)
					wantStatus, wantCount := render.ExitSuccessProblemFound, 1
					if config.disabled {
						wantStatus, wantCount = render.ExitSuccessNoProblem, 0
					}
					if status != wantStatus || stderr.Len() != 0 {
						t.Fatalf("exit=%d, want=%d; stderr=%s", status, wantStatus, stderr.String())
					}
					var diagnostics []struct{ Kind string }
					if err := json.Unmarshal(stdout.Bytes(), &diagnostics); err != nil {
						t.Fatalf("invalid JSON output: %v: %s", err, stdout.String())
					}
					if len(diagnostics) != wantCount || (wantCount == 1 && diagnostics[0].Kind != policy.name) {
						t.Fatalf("want %d %s findings; got %s", wantCount, policy.name, stdout.String())
					}
				})
			}
		}
	}
}
