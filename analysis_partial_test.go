package actionlint

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

	"github.com/google/go-cmp/cmp"
)

type analysisCancelRule struct {
	RuleBase
	shellcheck *externalCommand
	cancel     context.CancelFunc
}

func (rule *analysisCancelRule) VisitStep(step *Step) error {
	run, ok := step.Exec.(*ExecRun)
	if !ok || run.Shell == nil || run.Shell.Value != "python" {
		return nil
	}
	// Keep an already completed finding while cancelling another analyzer.
	if err := rule.shellcheck.wait(); err != nil {
		return err
	}
	rule.cancel()
	return context.Canceled
}

func TestAnalysisPartialFindingsOnCancellation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := Analyze(ctx, AnalysisRequest{
		Sources:           []SourceUnit{{Path: "cancelled.yml", Content: []byte(commandBadWorkflow + "        shell: bash\n      - run: print(1)\n        shell: python\n")}},
		ShellcheckOptions: &ExternalCommandOptions{Executable: &executable, Arguments: []string{"-test.run=^TestAnalysisPartialAnalyzerHelper$", "--", "shellcheck"}},
		OnRulesCreated: func(rules []Rule) []Rule {
			for _, rule := range rules {
				if shellcheck, ok := rule.(*RuleShellcheck); ok {
					return append(rules, &analysisCancelRule{NewRuleBase("cancel", ""), shellcheck.cmd, cancel})
				}
			}
			return rules
		},
	})
	if !errors.Is(err, context.Canceled) || result == nil {
		t.Fatalf("cancellation lost partial result or cause: %v, %v", result, err)
	}
	for _, rule := range []string{"expression", "shellcheck"} {
		if !slices.ContainsFunc(result.Diagnostics, func(d Diagnostic) bool { return d.Rule == rule && d.Path == "cancelled.yml" }) {
			t.Errorf("cancellation lost completed %s finding: %v", rule, result.Diagnostics)
		}
	}
}

func TestAnalysisPartialFindings(t *testing.T) {
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
`
	successful := commandBadWorkflow + "        shell: pwsh\n"
	for name, content := range map[string]string{"failing.yml": failing, "successful.yml": successful} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	options := func(tool string) *ExternalCommandOptions {
		return &ExternalCommandOptions{
			Executable: &executable,
			Arguments:  []string{"-test.run=^TestAnalysisPartialAnalyzerHelper$", "--", tool},
		}
	}
	for _, adapter := range []string{"analyze", "session"} {
		for _, failingFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failing-first=%t", adapter, failingFirst), func(t *testing.T) {
				sources := []SourceUnit{{Path: "successful.yml", Content: []byte(successful)}, {Path: "failing.yml", Content: []byte(failing)}}
				if failingFirst {
					slices.Reverse(sources)
				}
				var result *AnalysisResult
				var analysisErr error
				if adapter == "analyze" {
					result, analysisErr = Analyze(t.Context(), AnalysisRequest{
						Sources: sources, WorkingDir: root,
						ShellcheckOptions: options("shellcheck"),
					})
				} else {
					session, err := NewAnalysisSession(AnalysisOptions{
						Context: t.Context(), WorkingDir: root, ConfigFile: configPath,
						ShellcheckOptions: options("shellcheck"),
					})
					if err != nil {
						t.Fatal(err)
					}
					paths := []string{filepath.Join(root, sources[0].Path), filepath.Join(root, sources[1].Path)}
					result, analysisErr = session.Files(paths, nil)
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
				want := []string{"successful.yml:expression", "failing.yml:expression", "failing.yml:shellcheck"}
				if failingFirst {
					want = append(want[1:], want[0])
				}
				if diff := cmp.Diff(want, findings); diff != "" {
					t.Fatalf("partial findings (-want +got):\n%s", diff)
				}
				if result.FileCount() != 2 {
					t.Fatalf("partial result lost source files: %d", result.FileCount())
				}
				if adapter == "session" && !slices.Contains(result.Inputs, configPath) {
					t.Fatalf("partial result lost explicit config input: %v", result.Inputs)
				}
			})
		}
	}
}

func TestAnalysisPartialAnalyzerHelper(t *testing.T) {
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
		fmt.Fprintln(os.Stdout, `{"comments":[{"line":2,"endLine":2,"column":1,"endColumn":2,"level":"warning","code":9999,"message":"retained ShellCheck finding"}]}`)
	default:
		t.Fatalf("unexpected analyzer %q", os.Args[separator+1])
	}
	os.Exit(0)
}
