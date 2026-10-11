package actionlint

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCachePolicyInlineSuppression(t *testing.T) {
	const header = "on: pull_request_target\n"
	const body = `jobs:
  test:
` + cachePolicySteps
	for _, tc := range []struct {
		name, declaration string
		kinds             []string
	}{
		{"trailing", "cache-mode: write # actionlint:ignore cache-write-untrusted -- only reviewed code runs\n", nil},
		{"preceding", `# actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs
cache-mode: write
`, nil},
		{"unrelated selector", "cache-mode: write # actionlint:ignore cache-operation -- unused test selector\n", []string{"cache-write-untrusted"}},
		{"missing reason", "cache-mode: write # actionlint:ignore cache-write-untrusted\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"empty reason", "cache-mode: write # actionlint:ignore cache-write-untrusted -- \n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"misspelled selector", "cache-mode: write # actionlint:ignore cache-write-untrustd -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"wildcard rejected", "cache-mode: write # actionlint:ignore * -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"unrelated rule does not suppress cache", "cache-mode: write # actionlint:ignore expression -- only reviewed code runs\n", []string{"cache-write-untrusted"}},
		{"wrong directive", "cache-mode: write # actionlint:disable cache-write-untrusted -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"standalone ignore", `# actionlint:ignore cache-write-untrusted -- only reviewed code runs
cache-mode: write
`, []string{"inline-suppression", "cache-write-untrusted"}},
		{"trailing next-line", "cache-mode: write # actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs\n", []string{"cache-write-untrusted", "inline-suppression"}},
		{"comma selectors", "cache-mode: write # actionlint:ignore cache-operation, cache-write-untrusted -- only reviewed code runs\n", nil},
		{"quoted text", `name: '# actionlint:ignore-next-line cache-write-untrusted -- not a directive'
cache-mode: write
`, []string{"cache-write-untrusted"}},
		{"block scalar text", `name: |
  # actionlint:ignore-next-line cache-write-untrusted -- not a directive
cache-mode: write
`, []string{"cache-write-untrusted"}},
		{"multiline quoted text", `name: 'example
  # actionlint:ignore-next-line cache-write-untrusted -- not a directive'
cache-mode: write
`, []string{"cache-write-untrusted"}},
		{"empty line separates directive", `# actionlint:ignore-next-line cache-write-untrusted -- only reviewed code runs

cache-mode: write
`, []string{"cache-write-untrusted"}},
		{"alias declaration", `env: {MODE: &mode write}
cache-mode: *mode # actionlint:ignore cache-write-untrusted -- not at the diagnostic location
`, []string{"cache-write-untrusted"}},
		{"anchor declaration", `env:
  MODE: &mode write # actionlint:ignore cache-write-untrusted -- only reviewed code runs
cache-mode: *mode
`, nil},
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
	if diff := cmp.Diff(errs[1:], filterInlineSuppressions(source, errs, nil), cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
	source = []byte("name: café # actionlint:ignore typo -- reason\n")
	got := filterInlineSuppressions(source, nil, nil)
	if len(got) != 1 || got[0].Line != 1 || got[0].Column != 12 {
		t.Fatalf("expected directive error at 1:12, got %v", got)
	}
}

func TestCachePolicySuppressionLayouts(t *testing.T) {
	const header = "on: pull_request_target\n"
	const body = `jobs:
  test:
` + cachePolicySteps
	const ignore = "# actionlint:ignore cache-write-untrusted -- reviewed"
	const next = "# actionlint:ignore-next-line cache-write-untrusted -- reviewed"
	for _, tc := range []struct {
		name, source string
		kinds        []string
	}{
		{"nearby comments", header + "# context\n" + next + `
cache-mode: write # value context
# job context
` + body, nil},
		{"intervening comment detaches", header + next + `
# context
cache-mode: write
` + body, []string{"cache-write-untrusted"}},
		{"only adjacent directive applies", header + "# actionlint:ignore-next-line expression -- detached\n" + next + `
cache-mode: write
` + body, nil},
		{"orphan directive", header + "cache-mode: write\n" + body + "# actionlint:ignore expression -- detached\n", []string{"cache-write-untrusted"}},
		{"flow mapping", header + "jobs: {test: {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}} " + ignore + "\n", nil},
		{"multiline flow mapping", header + `jobs: {test: {
  runs-on: ubuntu-latest, steps: [{run: echo ok}],
  cache-mode: write}} ` + ignore + "\n", nil},
		{"flow sequence", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    cache-mode: read
    steps: [{uses: actions/cache/save@v5, with: {path: .cache, key: test}}] # actionlint:ignore cache-operation -- reviewed
`, nil},
		{"multiline flow sequence", `on: push
jobs:
  test:
    runs-on: ubuntu-latest
    cache-mode: read
    steps: [
      {uses: actions/cache/save@v5, with: {path: .cache, key: test}}] # actionlint:ignore cache-operation -- reviewed
`, nil},
		{"flow comments repeated on nested collections", header + "jobs: {test: {\n  cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]} " + ignore + "\n} " + ignore + "\n", nil},
		{"flow comment before same comment in later document", header + `jobs: {test: {
  runs-on: ubuntu-latest, steps: [{run: echo ok}],
  cache-mode: write}} ` + ignore + "\n---\n{cache-mode: write} " + ignore + "\n", nil},
		{"flow comment before matching ordinary footer", header + `jobs: {test: {
  runs-on: ubuntu-latest, steps: [{run: echo ok}],
  cache-mode: write}} ` + ignore + "\n# } " + ignore + "\n", nil},
		{"closing line does not target earlier line", header + "jobs: {test: {cache-mode: write,\n  runs-on: ubuntu-latest, steps: [{run: echo ok}]}} " + ignore + "\n", []string{"cache-write-untrusted"}},
		{"anchor head", header + "env:\n  " + next + `
  MODE: &mode write
cache-mode: *mode
` + body, nil},
		{"alias head keeps anchor location", header + "env: {MODE: &mode write}\n" + next + `
cache-mode: *mode
` + body, []string{"cache-write-untrusted"}},
		{"key comment does not target value", header + "cache-mode: " + ignore + "\n  write\n" + body, []string{"cache-write-untrusted"}},
		{"value line comment", header + "cache-mode:\n  write " + ignore + "\n" + body, nil},
		{"value head comment", header + "cache-mode:\n  " + next + "\n  write\n" + body, nil},
		{"literal before same detached comment", header + "name: |\n  " + next + `
cache-mode: write

` + next + "\n" + body, []string{"cache-write-untrusted"}},
		{"detached flow prefix comment", header + `jobs: {first: &job
 # actionlint:ignore-next-line expression -- detached
 # context
 {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}
`, []string{"cache-write-untrusted"}},
		{"document markers", "---\n" + next + `
cache-mode: write
` + header + body + "...\n", nil},
		// Parse reads one document. Directives in later documents cannot affect it.
		{"second document ignored", header + "cache-mode: write\n" + body + `---
cache-mode: write # actionlint:ignore expression -- later document
`, []string{"cache-write-untrusted"}},
		{"document marker detaches", next + `
---
cache-mode: write
` + header + body, []string{"cache-write-untrusted"}},
		{"legal tab separation", header + "cache-mode:\twrite\t#\tactionlint:ignore cache-write-untrusted -- reviewed\n" + body, nil},
		{"unusual indentation", header + `jobs:
 test:
   cache-mode: write ` + ignore + `
   runs-on: ubuntu-latest
   steps:
    - run: echo ok
`, nil},
		{"UTF8 before directive", header + "env: {NOTE: café, MODE: &mode write} " + ignore + `
cache-mode: *mode
` + body, nil},
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
		{"unsupported rule", "unknown-rule -- reviewed", true},
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
					declaration = "# actionlint:ignore-next-line " + tc.suffix + `
cache-mode: write
`
				}
				errs := lintCachePolicy(t, "on: pull_request_target\n"+declaration+`jobs:
  test:
`+cachePolicySteps, "")
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
	const body = `jobs:
  test:
` + cachePolicySteps
	const directive = "# actionlint:ignore unknown-rule -- reviewed"
	for _, tc := range []struct {
		name, source string
		line         int
		policy       string
	}{
		{"flow opening", header + "jobs: { " + directive + `
 test: {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}}
`, 2, "cache-write-untrusted"},
		{"tagged anchored flow opening", header + "jobs: {first: &job !<tag:yaml.org,2002:map> { " + directive + `
 cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}
`, 2, "cache-write-untrusted"},
		{"multiline anchored flow opening", header + `jobs:
 first: &job
  { ` + directive + `
    cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}
 second: *job
`, 4, "cache-write-untrusted"},
		{"flow anchor comment", header + "jobs: {first: &job " + directive + "\n  {cache-mode: write, runs-on: ubuntu-latest, steps: [{run: echo ok}]}, second: *job}\n", 2, "cache-write-untrusted"},
		{"flow closing", header + `jobs: {test: {
 runs-on: ubuntu-latest, steps: [{run: echo ok}],
 cache-mode: write}} ` + directive + "\n", 4, "cache-write-untrusted"},
		{"UTF8 before sequence opening", "on: push\njobs: {test: {name: café, runs-on: ubuntu-latest, cache-mode: read, steps: [ " + directive + "\n {uses: actions/cache/save@v5, with: {path: .cache, key: test}}]}}\n", 2, "cache-operation"},
		{"flow sequence closing", `on: push
jobs: {test: {runs-on: ubuntu-latest, cache-mode: read, steps: [
 {uses: actions/cache/save@v5, with: {path: .cache, key: test}}]}} ` + directive + "\n", 3, "cache-operation"},
		{"mapping key", header + "cache-mode: " + directive + "\n  write\n" + body, 2, "cache-write-untrusted"},
		{"mapping value", header + "cache-mode:\n  write " + directive + "\n" + body, 3, "cache-write-untrusted"},
		{"anchor head", header + `env:
  # actionlint:ignore-next-line unknown-rule -- reviewed
  MODE: &mode write
cache-mode: *mode
` + body, 3, "cache-write-untrusted"},
		{"quoted closing", header + "cache-mode: \"wr\\\n  ite\" " + directive + "\n" + body, 3, "cache-write-untrusted"},
		{"block scalar header", header + "name: | " + directive + "\n  " + directive + `
cache-mode: write
` + body, 2, "cache-write-untrusted"},
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
