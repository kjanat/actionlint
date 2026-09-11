package actionlint

import (
	"encoding/json"
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
	if diff := cmp.Diff(errs[1:], filterCachePolicySuppressions(source, errs), cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := filterCachePolicySuppressions(source, nil)
	if len(got) != 1 || got[0].Line != 1 || got[0].Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}
