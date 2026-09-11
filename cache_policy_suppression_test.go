package actionlint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyCommandOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, exception := range []string{"", " # actionlint:ignore cache-write-untrusted -- reviewed code only"} {
		source := "on: pull_request_target\ncache-mode: write" + exception + "\njobs:\n  test:\n" + cachePolicySteps
		var stdout, stderr strings.Builder
		cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
		status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}", "-"})
		if stderr.Len() != 0 {
			t.Fatalf("unexpected stderr: %s", &stderr)
		}
		var diagnostics []struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
			t.Fatal(err)
		}
		if exception == "" {
			if status != ExitStatusSuccessProblemFound || len(diagnostics) != 1 || diagnostics[0].Kind != "cache-write-untrusted" {
				t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
			}
		} else if status != ExitStatusSuccessNoProblem || len(diagnostics) != 0 {
			t.Fatalf("status=%d diagnostics=%v", status, diagnostics)
		}
	}
}

func TestCachePolicyInvalidSuppressionSARIF(t *testing.T) {
	t.Chdir(t.TempDir())
	source := "on: pull_request_target\ncache-mode: write # actionlint:ignore cache-write-untrusted\njobs:\n  test:\n" + cachePolicySteps
	var stdout, stderr strings.Builder
	cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
	status := cmd.Main([]string{"actionlint", "-shellcheck=", "-pyflakes=", "-format", SARIFTemplate(), "-"})
	if status != ExitStatusSuccessProblemFound || stderr.Len() != 0 {
		t.Fatalf("status=%d stderr=%s", status, &stderr)
	}
	var report struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Rules []struct{ ID string }
				}
			}
			Results []struct{ RuleID string }
		}
	}
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || len(report.Runs[0].Results) != 2 {
		t.Fatalf("expected policy and directive results: %s", &stdout)
	}
	descriptors := map[string]bool{}
	for _, rule := range report.Runs[0].Tool.Driver.Rules {
		descriptors[rule.ID] = true
	}
	for _, result := range report.Runs[0].Results {
		if !descriptors[result.RuleID] {
			t.Errorf("SARIF result %q has no rule descriptor", result.RuleID)
		}
	}
}

func TestCachePolicyInlineSuppression(t *testing.T) {
	const header = "on: pull_request_target\n"
	const body = "jobs:\n  test:\n" + cachePolicySteps
	for _, tc := range []struct {
		name, declaration string
		kinds             []string
	}{
		{"trailing", "cache-mode: write # actionlint:ignore cache-write-untrusted -- only reviewed code runs\n", nil},
		{"preceding", "# actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs\ncache-mode: write\n", nil},
		{"unrelated selector", "cache-mode: write # actionlint:ignore cache-operation -- unused test selector\n", []string{"cache-write-untrusted"}},
		{"missing reason", "cache-mode: write # actionlint:ignore cache-write-untrusted\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"empty reason", "cache-mode: write # actionlint:ignore cache-write-untrusted -- \n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"misspelled selector", "cache-mode: write # actionlint:ignore cache-write-untrustd -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"wildcard rejected", "cache-mode: write # actionlint:ignore * -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"unrelated rule rejected", "cache-mode: write # actionlint:ignore expression -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"wrong directive", "cache-mode: write # actionlint:disable cache-write-untrusted -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"standalone ignore", "# actionlint:ignore cache-write-untrusted -- only reviewed code runs\ncache-mode: write\n", []string{"inline-suppression", "cache-write-untrusted"}},
		{"trailing next-line", "cache-mode: write # actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"comma selectors", "cache-mode: write # actionlint:ignore cache-operation, cache-write-untrusted -- only reviewed code runs\n", nil},
		{"quoted text", "name: '# actionlint:ignore-next-line cache-write-untrusted -- not a directive'\ncache-mode: write\n", []string{"cache-write-untrusted"}},
		{"block scalar text", "name: |\n  # actionlint:ignore-next-line cache-write-untrusted -- not a directive\ncache-mode: write\n", []string{"cache-write-untrusted"}},
		{"multiline quoted text", "name: 'example\n  # actionlint:ignore-next-line cache-write-untrusted -- not a directive'\ncache-mode: write\n", []string{"cache-write-untrusted"}},
		{"empty line separates directive", "# actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs\n\ncache-mode: write\n", []string{"cache-write-untrusted"}},
		{"alias declaration", "env: {MODE: &mode write}\ncache-mode: *mode # actionlint:ignore cache-write-untrusted -- not at the diagnostic location\n", []string{"cache-write-untrusted"}},
		{"anchor declaration", "env:\n  MODE: &mode write # actionlint:ignore cache-write-untrusted -- only reviewed code runs\ncache-mode: *mode\n", nil},
	} {
		for _, crlf := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/LF", true: "/CRLF"}[crlf], func(t *testing.T) {
				source := header + tc.declaration + body
				if crlf {
					source = strings.ReplaceAll(source, "\n", "\r\n")
				}
				var kinds []string
				for _, err := range lintCachePolicy(t, source, "") {
					kinds = append(kinds, err.Kind)
				}
				if diff := cmp.Diff(tc.kinds, kinds); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestCachePolicyInlineSuppressionScope(t *testing.T) {
	source := `on: pull_request_target
jobs:
  reviewed:
    # actionlint:ignore-next-line cache-call-unrestricted -- callee controls a reviewed cache namespace
    uses: example/repo/.github/workflows/build.yaml@main
  other:
    uses: example/repo/.github/workflows/build.yaml@main
  operation:
    runs-on: ubuntu-latest
    cache-mode: read
    steps:
      - uses: actions/cache/save@v5 # actionlint:ignore cache-operation -- check a skipped save in this test
        with: {path: .cache, key: test}
      - uses: actions/cache/save@v5
        with: {path: .cache, key: test}
      - run: echo '${{ secrets.MISSING }}'
`
	errs := lintCachePolicy(t, source, "config-secrets: []")
	var got []string
	for _, err := range errs {
		got = append(got, err.Kind)
	}
	if diff := cmp.Diff([]string{"cache-call-unrestricted", "cache-operation", "expression"}, got); diff != "" {
		t.Fatal(diff)
	}
	if errs[0].Line != 7 || errs[1].Line != 14 || errs[2].Line != 16 {
		t.Fatal(errs)
	}
}

func TestCachePolicyInlineSuppressionKeepsOtherDiagnostics(t *testing.T) {
	source := []byte("name: café # actionlint:ignore cache-operation -- reviewed\n")
	errs := []*Error{
		{Line: 1, Kind: "cache-operation"},
		{Line: 1, Kind: "cache-operation", source: []byte("other file")},
		{Line: 1, Kind: "syntax-check"},
		{Line: 2, Kind: "cache-operation"},
	}
	if diff := cmp.Diff(errs[1:], filterCachePolicySuppressions(source, errs, nil), cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := filterCachePolicySuppressions(source, nil, nil)
	if len(got) != 1 || got[0].Line != 1 || got[0].Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}

func TestCachePolicySuppressionLayouts(t *testing.T) {
	const header = "on: pull_request_target\n"
	const body = "jobs:\n  test:\n" + cachePolicySteps
	const ignore = "# actionlint:ignore cache-write-untrusted -- reviewed"
	const next = "# actionlint:ignore-next-line cache-write-untrusted -- reviewed"
	for _, tc := range []struct {
		name, source string
		kinds        []string
	}{
		{"nearby comments", header + "# context\n" + next + "\ncache-mode: write # value context\n# job context\n" + body, nil},
		{"intervening comment detaches", header + next + "\n# context\ncache-mode: write\n" + body, []string{"cache-write-untrusted"}},
		{"only adjacent directive applies", header + "# actionlint:ignore-next-line expression -- detached\n" + next + "\ncache-mode: write\n" + body, nil},
		{"orphan directive", header + "cache-mode: write\n" + body + "# actionlint:ignore expression -- detached\n", []string{"cache-write-untrusted"}},
		{"flow mapping", header + "jobs: {test: {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}} " + ignore + "\n", nil},
		{"multiline flow mapping", header + "jobs: {test: {\n  runs-on: ubuntu-latest, steps: [{run: echo ok}],\n  cache-mode: write}} " + ignore + "\n", nil},
		{"flow sequence", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    cache-mode: read\n    steps: [{uses: actions/cache/save@v5, with: {path: .cache, key: test}}] # actionlint:ignore cache-operation -- reviewed\n", nil},
		{"multiline flow sequence", "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    cache-mode: read\n    steps: [\n      {uses: actions/cache/save@v5, with: {path: .cache, key: test}}] # actionlint:ignore cache-operation -- reviewed\n", nil},
		{"flow comments repeated on nested collections", header + "jobs: {test: {\n  cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]} " + ignore + "\n} " + ignore + "\n", nil},
		{"flow comment before same comment in later document", header + "jobs: {test: {\n  runs-on: ubuntu-latest, steps: [{run: echo ok}],\n  cache-mode: write}} " + ignore + "\n---\n{cache-mode: write} " + ignore + "\n", nil},
		{"flow comment before matching ordinary footer", header + "jobs: {test: {\n  runs-on: ubuntu-latest, steps: [{run: echo ok}],\n  cache-mode: write}} " + ignore + "\n# } " + ignore + "\n", nil},
		{"closing line does not target earlier line", header + "jobs: {test: {cache-mode: write,\n  runs-on: ubuntu-latest, steps: [{run: echo ok}]}} " + ignore + "\n", []string{"cache-write-untrusted"}},
		{"anchor head", header + "env:\n  " + next + "\n  MODE: &mode write\ncache-mode: *mode\n" + body, nil},
		{"alias head keeps anchor location", header + "env: {MODE: &mode write}\n" + next + "\ncache-mode: *mode\n" + body, []string{"cache-write-untrusted"}},
		{"key comment does not target value", header + "cache-mode: " + ignore + "\n  write\n" + body, []string{"cache-write-untrusted"}},
		{"value line comment", header + "cache-mode:\n  write " + ignore + "\n" + body, nil},
		{"value head comment", header + "cache-mode:\n  " + next + "\n  write\n" + body, nil},
		{"literal before same detached comment", header + "name: |\n  " + next + "\ncache-mode: write\n\n" + next + "\n" + body, []string{"cache-write-untrusted"}},
		{"detached flow prefix comment", header + "jobs: {first: &job\n # actionlint:ignore-next-line expression -- detached\n # context\n {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}\n", []string{"cache-write-untrusted"}},
		{"document markers", "---\n" + next + "\ncache-mode: write\n" + header + body + "...\n", nil},
		// Parse reads one document. Directives in later documents cannot affect it.
		{"second document ignored", header + "cache-mode: write\n" + body + "---\ncache-mode: write # actionlint:ignore expression -- later document\n", []string{"cache-write-untrusted"}},
		{"document marker detaches", next + "\n---\ncache-mode: write\n" + header + body, []string{"cache-write-untrusted"}},
		{"legal tab separation", header + "cache-mode:\twrite\t#\tactionlint:ignore cache-write-untrusted -- reviewed\n" + body, nil},
		{"unusual indentation", header + "jobs:\n test:\n   cache-mode: write " + ignore + "\n   runs-on: ubuntu-latest\n   steps:\n    - run: echo ok\n", nil},
		{"UTF8 before directive", header + "env: {NOTE: café, MODE: &mode write} " + ignore + "\ncache-mode: *mode\n" + body, nil},
		{"no final newline", header + body + "cache-mode: write " + ignore, nil},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[ending], func(t *testing.T) {
				var kinds []string
				for _, err := range lintCachePolicy(t, strings.ReplaceAll(tc.source, "\n", ending), "") {
					kinds = append(kinds, err.Kind)
				}
				if diff := cmp.Diff(tc.kinds, kinds); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestCachePolicySuppressionGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, suffix string
		invalid      bool
	}{
		{"unsupported rule", "expression -- reviewed", true},
		{"duplicate selector", "cache-write-untrusted,cache-write-untrusted -- reviewed", false},
		{"leading empty selector", ",cache-write-untrusted -- reviewed", true},
		{"trailing empty selector", "cache-write-untrusted, -- reviewed", true},
		{"middle empty selector", "cache-write-untrusted,,cache-operation -- reviewed", true},
		{"no selector", " -- reviewed", true},
		{"directive in reason", "cache-write-untrusted -- reviewed actionlint:ignore expression", false},
		{"separator in reason", "cache-write-untrusted -- reviewed -- restricted to reviewed code", false},
		{"directive in selectors", "cache-write-untrusted actionlint:ignore expression -- reviewed", true},
	} {
		for _, preceding := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/trailing", true: "/preceding"}[preceding], func(t *testing.T) {
				declaration := "cache-mode: write # actionlint:ignore " + tc.suffix + "\n"
				if preceding {
					declaration = "# actionlint:ignore-next-line " + tc.suffix + "\ncache-mode: write\n"
				}
				errs := lintCachePolicy(t, "on: pull_request_target\n"+declaration+"jobs:\n  test:\n"+cachePolicySteps, "")
				if !tc.invalid {
					if len(errs) != 0 {
						t.Fatal(errs)
					}
					return
				}
				counts := map[string]int{}
				for _, err := range errs {
					counts[err.Kind]++
				}
				if diff := cmp.Diff(map[string]int{"cache-write-untrusted": 1, "inline-suppression": 1}, counts); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestCachePolicyUnsupportedSuppressionPlacements(t *testing.T) {
	const header = "on: pull_request_target\n"
	const body = "jobs:\n  test:\n" + cachePolicySteps
	const directive = "# actionlint:ignore expression -- reviewed"
	for _, tc := range []struct {
		name, source string
		line         int
		policy       string
	}{
		{"flow opening", header + "jobs: { " + directive + "\n test: {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}}\n", 2, "cache-write-untrusted"},
		{"tagged anchored flow opening", header + "jobs: {first: &job !<tag:yaml.org,2002:map> { " + directive + "\n cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}\n", 2, "cache-write-untrusted"},
		{"multiline anchored flow opening", header + "jobs:\n first: &job\n  { " + directive + "\n    cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}\n second: *job\n", 4, "cache-write-untrusted"},
		{"flow anchor comment", header + "jobs: {first: &job " + directive + "\n  {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}\n", 2, "cache-write-untrusted"},
		{"flow closing", header + "jobs: {test: {\n runs-on: ubuntu-latest, steps: [{run: echo ok}],\n cache-mode: write}} " + directive + "\n", 4, "cache-write-untrusted"},
		{"UTF8 before sequence opening", "on: push\njobs: {test: {name: café, runs-on: ubuntu-latest, cache-mode: read, steps: [ " + directive + "\n {uses: actions/cache/save@v5, with: {path: .cache, key: test}}]}}\n", 2, "cache-operation"},
		{"flow sequence closing", "on: push\njobs: {test: {runs-on: ubuntu-latest, cache-mode: read, steps: [\n {uses: actions/cache/save@v5, with: {path: .cache, key: test}}]}} " + directive + "\n", 3, "cache-operation"},
		{"mapping key", header + "cache-mode: " + directive + "\n  write\n" + body, 2, "cache-write-untrusted"},
		{"mapping value", header + "cache-mode:\n  write " + directive + "\n" + body, 3, "cache-write-untrusted"},
		{"anchor head", header + "env:\n  # actionlint:ignore-next-line expression -- reviewed\n  MODE: &mode write\ncache-mode: *mode\n" + body, 3, "cache-write-untrusted"},
		{"quoted closing", header + "cache-mode: \"wr\\\n  ite\" " + directive + "\n" + body, 3, "cache-write-untrusted"},
		{"block scalar header", header + "name: | " + directive + "\n  " + directive + "\ncache-mode: write\n" + body, 2, "cache-write-untrusted"},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[ending], func(t *testing.T) {
				errs := lintCachePolicy(t, strings.ReplaceAll(tc.source, "\n", ending), "")
				counts := map[string]int{}
				for _, err := range errs {
					counts[err.Kind]++
					if err.Kind == "inline-suppression" && err.Line != tc.line {
						t.Errorf("directive at line %d, want %d", err.Line, tc.line)
					}
				}
				if diff := cmp.Diff(map[string]int{tc.policy: 1, "inline-suppression": 1}, counts); diff != "" {
					t.Fatalf("%s\nerrors: %v", diff, errs)
				}
			})
		}
	}
}

func TestCachePolicySuppressionFilterPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, directive, cliIgnore, pathIgnore string
		kinds                                  []string
	}{
		{"redundant CLI ignore", "cache-write-untrusted -- reviewed", "poison caches", "", nil},
		{"CLI ignore keeps directive errors", "typo -- reviewed", "poison caches", "", []string{"inline-suppression"}},
		{"CLI ignore removes directive errors", "typo -- reviewed", "unknown cache policy rule", "", []string{"cache-write-untrusted"}},
		{"redundant path ignore", "cache-write-untrusted -- reviewed", "", "poison caches", nil},
		{"path ignore keeps directive errors", "typo -- reviewed", "", "poison caches", []string{"inline-suppression"}},
		{"path ignore removes directive errors", "typo -- reviewed", "", "unknown cache policy rule", []string{"cache-write-untrusted"}},
		{"all filters overlap", "cache-operation,cache-write-untrusted -- reviewed", "poison caches", "poison caches", nil},
		{"separate filters remove both errors", "typo -- reviewed", "poison caches", "unknown cache policy rule", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			config := filepath.Join(t.TempDir(), "actionlint.yaml")
			if tc.pathIgnore != "" {
				if err := os.WriteFile(config, []byte("paths:\n  'test.yaml':\n    ignore: ['"+tc.pathIgnore+"']\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source := "on: pull_request_target\ncache-mode: write # actionlint:ignore " + tc.directive + "\njobs:\n  test:\n" + cachePolicySteps
			var stdout, stderr strings.Builder
			cmd := Command{Stdin: strings.NewReader(source), Stdout: &stdout, Stderr: &stderr}
			args := []string{"actionlint", "-shellcheck=", "-pyflakes=", "-no-color", "-format", "{{json .}}", "-stdin-filename", "test.yaml"}
			if tc.pathIgnore != "" {
				args = append(args, "-config-file", config)
			}
			if tc.cliIgnore != "" {
				args = append(args, "-ignore", tc.cliIgnore)
			}
			status := cmd.Main(append(args, "-"))
			wantStatus := ExitStatusSuccessNoProblem
			if len(tc.kinds) > 0 {
				wantStatus = ExitStatusSuccessProblemFound
			}
			if status != wantStatus || stderr.Len() != 0 {
				t.Fatalf("status=%d, want %d; stderr=%s", status, wantStatus, &stderr)
			}
			var diagnostics []struct{ Kind string }
			if err := json.Unmarshal([]byte(stdout.String()), &diagnostics); err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, diagnostic := range diagnostics {
				kinds = append(kinds, diagnostic.Kind)
			}
			if diff := cmp.Diff(tc.kinds, kinds); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
