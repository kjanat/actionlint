package checking_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	processconfig "actionlint.kjanat.dev/v2/internal/integrations/process/config"
	diagmodel "actionlint.kjanat.dev/v2/internal/results"

	"github.com/google/go-cmp/cmp"
)

func TestAnalysisPartialFindingsOnCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := checking.Analyze(ctx, checking.Options{Tools: registry.Registrations(registry.Selections{"shellcheck": {Runner: func([]string, string) ([]byte, error) {
		cancel()
		return []byte(`{"comments":[{"line":3,"endLine":3,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"reported ShellCheck finding"}]}`), nil
	}}})}, []workspace.Source{{Path: "cancelled.yml", Content: []byte(strings.Replace(badWorkflow, "permissions: {}\n", "", 1) + `        shell: bash
      - run: print(1)
        shell: python
permissions: {}
`)}})
	if !errors.Is(err, context.Canceled) || result == nil {
		t.Fatalf("cancellation lost partial result or cause: %v, %v", result, err)
	}
	for _, rule := range []string{"expression-reference", "shellcheck"} {
		if !slices.ContainsFunc(result.Diagnostics, func(d diagmodel.Diagnostic) bool { return d.Rule == rule && d.Path == "cancelled.yml" }) {
			t.Errorf("cancellation lost completed %s finding: %v", rule, result.Diagnostics)
		}
	}
}

func TestAnalysisPartialFindings(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yml")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	failing := `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo '${{ missing.value }}' FAIL_ANALYZER
        shell: bash
      - run: echo keep_shellcheck_finding
        shell: bash
permissions: {}
`
	successful := strings.Replace(badWorkflow, "permissions: {}\n", "        shell: pwsh\npermissions: {}\n", 1)
	for name, content := range map[string]string{"failing.yml": failing, "successful.yml": successful} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	options := func(tool string) *processconfig.Options {
		return &processconfig.Options{
			Executable: &executable,
			Arguments:  []string{"-test.run=^TestAnalysisPartialAnalyzerHelper$", "--", tool},
		}
	}
	for _, adapter := range []string{"sources", "files"} {
		for _, failingFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failing-first=%t", adapter, failingFirst), func(t *testing.T) {
				t.Parallel()
				sources := []workspace.Source{{Path: "successful.yml", Content: []byte(successful)}, {Path: "failing.yml", Content: []byte(failing)}}
				if failingFirst {
					slices.Reverse(sources)
				}
				var result *checking.Result
				var analysisErr error
				if adapter == "sources" {
					result, analysisErr = checking.Analyze(t.Context(), checking.Options{WorkingDir: root, Tools: registry.Registrations(registry.Selections{"shellcheck": {Process: options("shellcheck")}})}, sources)
				} else {
					session := checking.Options{WorkingDir: root, ConfigFile: configPath, Tools: registry.Registrations(registry.Selections{"shellcheck": {Process: options("shellcheck")}})}

					paths := []string{filepath.Join(root, sources[0].Path), filepath.Join(root, sources[1].Path)}
					result, analysisErr = checking.Check(t.Context(), session, inputmodel.Request{Kind: inputmodel.Files, Paths: paths})
				}
				if analysisErr == nil || !strings.Contains(analysisErr.Error(), "exited with status 2") {
					t.Fatalf("missing analyzer process failure: %v", analysisErr)
				}
				if result == nil {
					t.Fatal("analyzer failure discarded partial result")
				}
				var findings []string
				for _, diagnostic := range result.Diagnostics {
					findings = append(findings, diagnostic.Path+":"+diagnostic.Rule)
				}
				want := []string{"successful.yml:expression-reference", "failing.yml:expression-reference", "failing.yml:shellcheck"}
				if failingFirst {
					want = append(want[1:], want[0])
				}
				if diff := cmp.Diff(want, findings); diff != "" {
					t.Fatalf("partial findings (-want +got):\n%s", diff)
				}
				if result.FileCount != 2 {
					t.Fatalf("partial result lost source files: %d", result.FileCount)
				}
				if adapter == "files" && !slices.Contains(result.Inputs, configPath) {
					t.Fatalf("partial result lost explicit config input: %v", result.Inputs)
				}
			})
		}
	}
}

func TestAnalysisPartialAnalyzerHelper(t *testing.T) {
	t.Parallel()
	separator := slices.Index(os.Args, "--")
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	script, err := io.ReadAll(os.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(script), "FAIL_ANALYZER") {
		os.Exit(2)
	}
	switch os.Args[separator+1] {
	case "shellcheck":
		fmt.Fprintln(os.Stdout, `{"comments":[{"line":2,"endLine":2,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"reported ShellCheck finding"}]}`)
	default:
		t.Fatalf("unexpected analyzer %q", os.Args[separator+1])
	}
	os.Exit(0)
}
