package actionlint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDisallowSuppressionsCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name, config string
		kinds        []string
	}{
		{"absent", "", nil}, {"empty policy", "policy: {}", nil}, {"null policy", "policy: null", nil},
		{"null", "policy: {disallow-suppressions: null}", nil},
		{"false", "policy: {disallow-suppressions: false}", nil},
		{"true", "policy: {disallow-suppressions: true}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"mapping", "policy: {disallow-suppressions: {}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"all", "policy: {disallow-suppressions: {report: all}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"suppression", "policy: {disallow-suppressions: {report: suppression}}", []string{"disallow-suppressions"}},
		{"violation", "policy: {disallow-suppressions: {report: violation}}", []string{"cache-call-unrestricted"}},
		{"selected", "policy: {disallow-suppressions: {rules: [cache-call-unrestricted]}}", []string{"cache-call-unrestricted", "disallow-suppressions"}},
		{"other rule", "policy: {disallow-suppressions: {rules: [cache-operation]}}", nil},
		{"disabled underlying", "policy: {cache-call-unrestricted: false, disallow-suppressions: true}", []string{"disallow-suppressions"}},
		{"disabled underlying violation only", "policy: {cache-call-unrestricted: false, disallow-suppressions: {report: violation}}", nil},
	} {
		for _, preceding := range []bool{false, true} {
			for _, discovery := range []bool{false, true} {
				name := tc.name + map[bool]string{false: "/trailing", true: "/preceding"}[preceding] + map[bool]string{false: "/explicit", true: "/discovery"}[discovery]
				t.Run(name, func(t *testing.T) {
					t.Chdir(t.TempDir())
					configPath := "config.yaml"
					if discovery {
						if err := os.Mkdir(".git", 0755); err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Join(".github", "workflows"), 0755); err != nil {
							t.Fatal(err)
						}
						configPath = filepath.Join(".github", "actionlint.yaml")
					}
					if err := os.WriteFile(configPath, []byte(tc.config), 0600); err != nil {
						t.Fatal(err)
					}
					job := "  report: { uses: example/repository/.github/workflows/report.yml@main }"
					body := job + " # actionlint:ignore cache-call-unrestricted -- reviewed callee\n"
					if preceding {
						body = "  # actionlint:ignore-next-line cache-call-unrestricted -- reviewed callee\n" + job + "\n"
					}
					var stdout, stderr strings.Builder
					source := "on: pull_request_target\njobs:\n" + body
					cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
					args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}"}
					input := "-"
					if !discovery {
						args = append(args, "-config-file", configPath)
					} else {
						input = filepath.Join(".github", "workflows", "test.yaml")
						if err := os.WriteFile(input, []byte(source), 0600); err != nil {
							t.Fatal(err)
						}
					}
					status := cmd.Main(append(args, input))
					var diagnostics []struct {
						Kind string
						Line int
					}
					if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
						t.Fatal(err)
					}
					var kinds []string
					for _, d := range diagnostics {
						kinds = append(kinds, d.Kind)
						wantLine := 3
						if preceding && d.Kind == "cache-call-unrestricted" {
							wantLine = 4
						}
						if d.Line != wantLine {
							t.Errorf("%s line = %d, want %d", d.Kind, d.Line, wantLine)
						}
					}
					// Source order differs between trailing and preceding directives.
					if diff := cmp.Diff(kindCounts(tc.kinds), kindCounts(kinds)); diff != "" {
						t.Fatal(diff)
					}
					wantStatus := ExitStatusSuccessNoProblem
					if len(tc.kinds) != 0 {
						wantStatus = ExitStatusSuccessProblemFound
					}
					if status != wantStatus || stderr.Len() != 0 {
						t.Fatalf("status=%d, want=%d; stderr=%s", status, wantStatus, &stderr)
					}
				})
			}
		}
	}
}

func kindCounts(kinds []string) map[string]int {
	counts := map[string]int{}
	for _, kind := range kinds {
		counts[kind]++
	}
	return counts
}

func TestDisallowSuppressionsInteractions(t *testing.T) {
	const body = "\njobs:\n  test:\n" + cachePolicySteps
	for _, tc := range []struct {
		name, declaration, config string
		kinds                     []string
	}{
		{"unused", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "true", []string{"disallow-suppressions"}},
		{"unused violation only", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "{report: violation}", nil},
		{"missing reason", "cache-mode: write # actionlint:ignore cache-write-untrusted", "true", []string{"cache-write-untrusted", "inline-suppression"}},
		{"cannot exempt restriction", "cache-mode: write # actionlint:ignore disallow-suppressions, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "inline-suppression"}},
		{"duplicates", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"unselected still suppressed", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-operation]}", []string{"disallow-suppressions"}},
		{"selected restored", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-write-untrusted]}", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"detached", "# actionlint:ignore-next-line cache-write-untrusted -- reviewed\n\ncache-mode: write", "true", []string{"cache-write-untrusted"}},
		{"quoted", "name: '# actionlint:ignore cache-write-untrusted -- text'\ncache-mode: write", "true", []string{"cache-write-untrusted"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[ending], func(t *testing.T) {
				source := strings.ReplaceAll("on: pull_request_target\n"+tc.declaration+body, "\n", ending)
				var kinds []string
				for _, err := range lintCachePolicy(t, source, "policy: {disallow-suppressions: "+tc.config+"}") {
					kinds = append(kinds, err.Kind)
				}
				if diff := cmp.Diff(kindCounts(tc.kinds), kindCounts(kinds)); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestDisallowSuppressionsDiagnostic(t *testing.T) {
	const source = "name: café # actionlint:ignore cache-operation, cache-operation -- reviewed"
	cfg, err := ParseConfig([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	got := filterInlineSuppressions([]byte(source), nil, cfg.Policy.DisallowSuppressions)
	if len(got) != 1 {
		t.Fatalf("expected one directive diagnostic, got %v", got)
	}
	want := &Error{
		Line: 1, Column: 12, Kind: "disallow-suppressions",
		Message: "inline suppression of cache-operation is disallowed by policy.disallow-suppressions",
	}
	if diff := cmp.Diff(want, got[0], cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
}

func TestDisallowSuppressionsOutputAndFilters(t *testing.T) {
	t.Chdir(t.TempDir())
	const source = "on: pull_request_target\ncache-mode: write # actionlint:ignore cache-write-untrusted -- reviewed\njobs:\n  test:\n" + cachePolicySteps
	const forbidden = "disallowed by policy"
	const violation = "poison caches"
	for _, tc := range []struct {
		name, cliIgnore, configIgnore, format string
		want                                  []string
	}{
		{"SARIF", "", "", SARIFTemplate(), []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"CLI removes directive", forbidden, "", "{{json .}}", []string{"cache-write-untrusted"}},
		{"CLI removes violation", violation, "", "{{json .}}", []string{"disallow-suppressions"}},
		{"path removes directive", "", "paths: {'test.yaml': {ignore: ['" + forbidden + "']}}", "{{json .}}", []string{"cache-write-untrusted"}},
		{"all paths remove violation", "", "paths: {'**': {ignore: ['" + violation + "']}}", "{{json .}}", []string{"disallow-suppressions"}},
		{"overlapping filters", forbidden, "paths: {'test.yaml': {ignore: ['" + violation + "']}}", "{{json .}}", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if err := os.WriteFile(config, []byte("policy: {disallow-suppressions: true}\n"+tc.configIgnore), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr strings.Builder
			cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", tc.format, "-config-file", config, "-stdin-filename", "test.yaml"}
			if tc.cliIgnore != "" {
				args = append(args, "-ignore", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := ExitStatusSuccessNoProblem
			if len(tc.want) != 0 {
				wantStatus = ExitStatusSuccessProblemFound
			}
			if status != wantStatus || stderr.Len() != 0 {
				t.Fatalf("status=%d, want=%d; stderr=%s", status, wantStatus, &stderr)
			}
			var kinds []string
			if tc.format == SARIFTemplate() {
				var report struct {
					Runs []struct {
						Tool struct {
							Driver struct{ Rules []struct{ ID string } }
						}
						Results []struct{ RuleID string }
					}
				}
				if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
					t.Fatal(err)
				}
				if len(report.Runs) != 1 {
					t.Fatalf("unexpected report: %s", &stdout)
				}
				descriptors := map[string]bool{}
				for _, rule := range report.Runs[0].Tool.Driver.Rules {
					descriptors[rule.ID] = true
				}
				for _, result := range report.Runs[0].Results {
					kinds = append(kinds, result.RuleID)
					if !descriptors[result.RuleID] {
						t.Errorf("missing descriptor: %s", result.RuleID)
					}
				}
			} else {
				var diagnostics []struct{ Kind string }
				if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
					t.Fatal(err)
				}
				for _, diagnostic := range diagnostics {
					kinds = append(kinds, diagnostic.Kind)
				}
			}
			if diff := cmp.Diff(kindCounts(tc.want), kindCounts(kinds)); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
