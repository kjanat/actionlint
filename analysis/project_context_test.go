package analysis_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/analysis"
	"actionlint.kjanat.dev/analyzer"
)

func TestInMemorySourcesRetainLocalReferenceContext(t *testing.T) {
	const caller = `on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./local
  call:
    uses: ./.github/workflows/reuse.yml
`
	const action = `name: local
description: local
inputs:
  token:
    description: required
    required: true
runs:
  using: composite
  steps:
    - run: echo hello
      shell: bash
`
	const reusable = `on:
  workflow_call:
    inputs:
      message:
        type: string
        required: true
jobs:
  run:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`
	for _, mode := range []string{"snapshots", "snapshot-over-reader", "reader-action"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".github", "actionlint.yaml"), []byte("invalid: ["), 0o600); err != nil {
				t.Fatal(err)
			}
			sources := []analysis.Source{
				{Path: ".github/workflows/main.yml", Content: []byte(caller)},
				{Path: ".github/workflows/reuse.yml", Content: []byte(reusable)},
			}
			if mode != "reader-action" {
				sources = append(sources, analysis.Source{Path: "local/action.yml", Content: []byte(action)})
			}
			observer := &invocationAnalyzer{}
			options := analysis.Options{WorkingDir: root, Analyzers: []analyzer.Registration{{Analyzer: observer}}}
			if mode != "snapshots" {
				options.ReadFile = func(path string) ([]byte, error) {
					if filepath.Clean(path) == filepath.Join(root, "local", "action.yml") {
						if mode == "reader-action" {
							return []byte(action), nil
						}
						return []byte("name: stale\ndescription: stale\nruns: {using: node24, main: index.js}\n"), nil
					}
					if filepath.Clean(path) == filepath.Join(root, ".github", "workflows", "reuse.yml") {
						return nil, errors.New("provided reusable workflow must override the reader")
					}
					return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrNotExist}
				}
			}
			result, err := analysis.Analyze(t.Context(), analysis.Request{Sources: sources, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			foundAction, foundWorkflow, foundInvoked := false, false, false
			for _, finding := range result.Diagnostics {
				foundAction = foundAction || finding.Rule == "action" && strings.Contains(finding.Message, "token")
				foundWorkflow = foundWorkflow || finding.Rule == "workflow-call" && strings.Contains(finding.Message, "message")
				foundInvoked = foundInvoked || finding.Rule == "composite-scripts" && strings.Contains(finding.Message, "./local")
			}
			if !foundAction || !foundWorkflow || !foundInvoked {
				t.Fatalf("local metadata or invoked facts lost: %+v", result.Diagnostics)
			}
			for _, path := range []string{"local/action.yml", ".github/workflows/reuse.yml"} {
				if !slices.Contains(result.Inputs, filepath.Join(root, filepath.FromSlash(path))) {
					t.Errorf("missing dependency %s: %v", path, result.Inputs)
				}
			}
		})
	}
}

func TestRelativeConfigFileUsesAnalysisWorkingDirectory(t *testing.T) {
	for _, repository := range []bool{false, true} {
		for _, relativeRoot := range []bool{false, true} {
			t.Run(fmt.Sprintf("repository=%t/relative-root=%t", repository, relativeRoot), func(t *testing.T) {
				root := t.TempDir()
				if repository {
					git, err := exec.LookPath("git")
					if err != nil {
						t.Skip("Git required for repository discovery")
					}
					command := exec.CommandContext(t.Context(), git, "init", "--quiet", root)
					command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, "unused-gitconfig"))
					if output, err := command.CombinedOutput(); err != nil {
						t.Fatalf("git init: %v: %s", err, output)
					}
				}
				for path, content := range map[string]string{
					".git/HEAD":                "ref: refs/heads/master\n",
					".github/workflows/ci.yml": workflow,
					"settings/actionlint.yaml": "extends: [base.yaml]\n",
					"settings/base.yaml":       "lint: {enabled: false}\n",
				} {
					full := filepath.Join(root, filepath.FromSlash(path))
					if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				workingDir := root
				if relativeRoot {
					t.Chdir(filepath.Dir(root))
					cwd, err := os.Getwd()
					if err != nil {
						t.Fatal(err)
					}
					workingDir, err = filepath.Rel(cwd, root)
					if err != nil {
						t.Fatal(err)
					}
				}
				options := analysis.FileOptions{
					WorkingDir: workingDir, Analyzers: []analyzer.Registration{{Analyzer: namedFindingAnalyzer("observed")}}, ReadFile: func(path string) ([]byte, error) {
						rel, err := filepath.Rel(root, path)
						if err != nil || !filepath.IsAbs(path) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
							return nil, fmt.Errorf("read escaped analysis root: %s", path)
						}
						return os.ReadFile(path)
					},
					ConfigFile: "settings/actionlint.yaml", SkipProjectConfig: true,
				}
				var result *analysis.Result
				var err error
				if repository {
					result, err = analysis.Repository(t.Context(), "", options)
				} else {
					result, err = analysis.Files(t.Context(), []string{filepath.Join(root, ".github", "workflows", "ci.yml")}, options)
				}
				if err != nil || result == nil || len(result.Diagnostics) != 0 || result.FileCount != 1 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				for _, path := range []string{"settings/actionlint.yaml", "settings/base.yaml"} {
					if !slices.Contains(result.Inputs, filepath.Join(root, filepath.FromSlash(path))) {
						t.Errorf("missing config provenance %s: %v", path, result.Inputs)
					}
				}
			})
		}
	}
}
