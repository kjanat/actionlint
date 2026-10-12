package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"actionlint.kjanat.dev"
)

const cachePolicySteps = `    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`

func TestCommandCachePolicyDefaults(t *testing.T) {
	for _, policy := range []struct{ name, workflow string }{
		{"cache-write-untrusted", `on: pull_request_target
cache-mode: write
jobs:
  build:
` + cachePolicySteps},
		{"cache-call-unrestricted", `on: pull_request_target
jobs:
  call:
    uses: example/repo/.github/workflows/build.yaml@main
`},
		{"cache-operation", `on: push
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
			{"empty policy", "policy: {}\n", false},
			{"null policy", "policy: null\n", false},
			{"null setting", fmt.Sprintf("policy: {%s: null}\n", policy.name), false},
			{"true setting", fmt.Sprintf("policy: {%s: true}\n", policy.name), false},
			{"false setting", fmt.Sprintf("policy: {%s: false}\n", policy.name), true},
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
					args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-format", "{{json .}}"}
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
					cmd := Command{Stdin: bytes.NewReader(nil), Stdout: &stdout, Stderr: &stderr}
					status := cmd.Main(args)
					wantStatus, wantCount := actionlint.ExitStatusSuccessProblemFound, 1
					if config.disabled {
						wantStatus, wantCount = actionlint.ExitStatusSuccessNoProblem, 0
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
