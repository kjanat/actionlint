package actionlint

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestInlineSuppressionMultilinePlainComments(t *testing.T) {
	for _, value := range []string{
		"name: one\n  two",
		"name: !!str\n  example",
		"name: &title\n  example",
		"name: &title !!str\n  example",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			for _, tc := range []struct {
				name, rule, config, kind string
			}{
				{"unsupported", "unknown-rule", "", "inline-suppression"},
				{"prohibited", "cache-operation", "policy: {disallow-suppressions: true}", "disallow-suppressions"},
			} {
				t.Run(value+"/"+ending+"/"+tc.name, func(t *testing.T) {
					source := "on: push\n" + value + " # actionlint:ignore " + tc.rule + " -- reviewed\n"
					if strings.Contains(value, "&title") {
						source += "run-name: *title\n"
					}
					source += "jobs:\n  test:\n" + cachePolicySteps
					errs := lintCachePolicy(t, strings.ReplaceAll(source, "\n", ending), tc.config)
					if len(errs) != 1 || errs[0].Kind != tc.kind || errs[0].Line != 3 {
						t.Fatalf("want one %s on line 3; got %v", tc.kind, errs)
					}
				})
			}
		}
	}
}

func TestInlineSuppressionStages(t *testing.T) {
	const source = "name: one\n  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused\nrun-name: |\n  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused\n"
	comments := collectInlineSuppressionComments([]byte(source))
	if len(comments) != 1 || comments[0].pos != (Pos{Line: 2, Col: 7}) || comments[0].standalone {
		t.Fatalf("want only the real closing comment at 2:7, got %+v", comments)
	}
	directive, err := parseInlineSuppression(comments[0])
	if err != nil {
		t.Fatal(err)
	}
	want := inlineSuppressionDirective{
		pos: Pos{Line: 2, Col: 7}, targetLine: 2,
		rules: []string{"cache-operation"}, reason: "reviewed -- intentionally unused",
	}
	if diff := cmp.Diff(want, directive, cmp.AllowUnexported(inlineSuppressionDirective{})); diff != "" {
		t.Fatal(diff)
	}
	findings := []*Error{
		{Line: 1, Kind: "cache-operation"},
		{Line: 2, Kind: "cache-operation"},
		{Line: 2, Kind: "cache-operation", source: []byte("callee")},
		{Line: 2, Kind: "expression"},
		{Line: 4, Kind: "cache-operation"},
	}
	got := applyInlineSuppressions(findings, []inlineSuppressionDirective{directive}, nil)
	if diff := cmp.Diff([]*Error{findings[0], findings[2], findings[3], findings[4]}, got, cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
}

func TestInlineSuppressionScalarPrefixes(t *testing.T) {
	const invalid = "# actionlint:ignore unknown-rule -- reviewed"
	const allowed = "# actionlint:ignore cache-operation -- reviewed"
	for _, tc := range []struct {
		name, value string
		line        int
	}{
		{"literal", "name: !!str\n  | " + invalid + "\n    title\n", 3},
		{"folded", "name: !!str\n  > " + invalid + "\n    title\n", 3},
		{"literal indicator", "name: !!str\n  |2- " + invalid + "\n    title\n", 3},
		{"property only", "name: !!str " + invalid + "\n  title\n", 2},
		{"property and value", "name: !!str " + invalid + "\n  title " + allowed + "\n", 2},
		{"property and block header", "name: !!str " + allowed + "\n  | " + invalid + "\n    title\n", 3},
		{"standalone before scalar", "name: !!str\n  # actionlint:ignore-next-line unknown-rule -- reviewed\n  title\n", 3},
		{"block body is text", "name: !!str\n  | " + invalid + "\n    title " + allowed + "\n", 3},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+ending, func(t *testing.T) {
				source := "on: push\n" + tc.value + "jobs:\n  test:\n" + cachePolicySteps
				errs := lintCachePolicy(t, strings.ReplaceAll(source, "\n", ending), "")
				if len(errs) != 1 || errs[0].Kind != "inline-suppression" || errs[0].Line != tc.line {
					t.Fatalf("want one inline-suppression at %d, got %v", tc.line, errs)
				}
			})
		}
	}
	// Identical comments on the property and value are separate directives.
	source := "on: push\nname: !!str " + allowed + "\n  title " + allowed + "\njobs:\n  test:\n" + cachePolicySteps
	errs := lintCachePolicy(t, source, "policy: {disallow-suppressions: true}")
	if len(errs) != 2 || errs[0].Kind != "disallow-suppressions" || errs[1].Kind != "disallow-suppressions" || errs[0].Line != 2 || errs[1].Line != 3 {
		t.Fatalf("want separate prohibited directives on lines 2 and 3, got %v", errs)
	}
}

func TestInlineSuppressionRegistry(t *testing.T) {
	for _, name := range InlineSuppressibleRules() {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(fmt.Sprintf("policy: {disallow-suppressions: {rules: [%s]}}", name)))
			if err != nil {
				t.Fatal(err)
			}
			comment := inlineSuppressionComment{
				pos: Pos{Line: 4, Col: 3}, standalone: true,
				text: "actionlint:ignore-next-line " + name + " -- reviewed",
			}
			directive, parseErr := parseInlineSuppression(comment)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			finding := &Error{Line: 5, Kind: name}
			if got := applyInlineSuppressions([]*Error{finding}, []inlineSuppressionDirective{directive}, nil); len(got) != 0 {
				t.Fatalf("registered rule was not suppressed: %v", got)
			}
			got := applyInlineSuppressions([]*Error{finding}, []inlineSuppressionDirective{directive}, cfg.Policy.DisallowSuppressions)
			if len(got) != 2 || got[0] != finding || got[1].Kind != "disallow-suppressions" || got[1].Line != 4 {
				t.Fatalf("restriction did not retain finding and report comment: %v", got)
			}
		})
	}
}

func TestInlineSuppressionBlockBody(t *testing.T) {
	for _, header := range []string{
		"run: | # actionlint:ignore expression,shellcheck -- reviewed",
		"run: >- # actionlint:ignore expression,shellcheck -- reviewed",
		"run: |2+ # actionlint:ignore expression,shellcheck -- reviewed",
		"# actionlint:ignore-next-line expression,shellcheck -- reviewed\nrun: |",
		"run: &script | # actionlint:ignore expression,shellcheck -- reviewed",
		"run: !!str\n  | # actionlint:ignore expression,shellcheck -- reviewed",
		"# actionlint:ignore-next-line expression,shellcheck -- reviewed\nrun: !!str\n  |",
		"# actionlint:ignore-next-line expression,shellcheck -- reviewed\nrun: &script\n  |2-",
		"# actionlint:ignore-next-line expression,shellcheck -- reviewed\nrun: &script !!str\n  >-",
		"run: # actionlint:ignore expression,shellcheck -- reviewed\n  !!str\n  |",
		"run:\n  !!str\n  &script # actionlint:ignore expression,shellcheck -- reviewed\n  |",
		"# actionlint:ignore-next-line expression,shellcheck -- reviewed\nrun:\n  &script\n  !!str\n  >-",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(header+ending, func(t *testing.T) {
				start := strings.Count(header, "\n") + 1
				source := strings.ReplaceAll(header+"\n    echo first\n    echo second\nnext: value\n", "\n", ending)
				findings := []*Error{
					{Line: start + 1, Kind: "expression"},
					{Line: start + 2, Kind: "shellcheck"},
					{Line: start + 2, Kind: "action"},
					{Line: start + 3, Kind: "expression"},
				}
				got := filterInlineSuppressions([]byte(source), findings, nil)
				if diff := cmp.Diff(findings[2:], got, cmp.AllowUnexported(Error{})); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestInlineSuppressionSplitPrefixIsolation(t *testing.T) {
	for _, header := range []string{
		"env: {SAFE: value} # actionlint:ignore expression,shellcheck -- unrelated\nrun:\n  !!str\n  |",
		"run:\n  !!str\n  |\n    # actionlint:ignore expression,shellcheck -- script text",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll(header+"\n    echo body\n", "\n", ending)
			line := strings.Count(header, "\n") + 2
			findings := []*Error{{Line: line, Kind: "expression"}, {Line: line, Kind: "shellcheck"}}
			if got := filterInlineSuppressions([]byte(source), findings, nil); len(got) != 2 {
				t.Fatalf("unrelated prefix suppressed body: %q: %+v", source, got)
			}
		}
	}
}

func TestInlineSuppressionMalformedYAML(t *testing.T) {
	for _, source := range []string{
		"jobs: [ # actionlint:ignore syntax-check -- intentional",
		"# actionlint:ignore-next-line syntax-check -- intentional\njobs: [",
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := []byte(strings.ReplaceAll(source, "\n", ending))
			_, findings := Parse(source)
			if len(findings) != 1 || findings[0].Kind != "syntax-check" {
				t.Fatalf("malformed fixture findings: %+v", findings)
			}
			if got := filterInlineSuppressions(source, findings, nil); len(got) != 0 {
				t.Fatalf("syntax finding was not suppressed: %+v", got)
			}
		}
	}
	for _, source := range []string{
		"name: '# actionlint:ignore syntax-check -- text'\njobs: [",
		"name: \"# actionlint:ignore syntax-check -- text\"\njobs: [",
		"name: \"first\n  # actionlint:ignore syntax-check -- text\n  last\"\njobs: [",
		"name: 'first\n  # actionlint:ignore syntax-check -- text\n  last'\njobs: [",
		"name: 'it''s\n  # actionlint:ignore syntax-check -- text\n  last'\njobs: [",
		"name: \"escaped \\\"\n  # actionlint:ignore syntax-check -- text\n  last\"\njobs: [",
		"run: |\n  # actionlint:ignore syntax-check -- script\njobs: [",
		"run: >-\n  print('''\n  # actionlint:ignore syntax-check -- script\n  ''')\njobs: [",
		"run: !!str\n  |\n    # actionlint:ignore syntax-check -- script\njobs: [",
		"run: !!str\n\n  |2\n  # actionlint:ignore syntax-check -- script\njobs: [",
		"run: !!str\n# header comment\n  |2\n  # actionlint:ignore syntax-check -- script\njobs: [",
		"steps:\n  - run: &script |2\n      # actionlint:ignore syntax-check -- script\njobs: [",
		"jobs: [\n# actionlint:ignore-next-line syntax-check -- no declaration",
		"jobs: [\n# actionlint:ignore-next-line syntax-check -- no declaration\n",
		"jobs: [\n# actionlint:ignore-next-line syntax-check -- blank\n\n  value",
		"jobs: [\n# actionlint:ignore-next-line syntax-check -- comment\n# another comment\n  value",
	} {
		if got := collectInlineSuppressionComments([]byte(source)); len(got) != 0 {
			t.Fatalf("scalar content became a directive in %q: %+v", source, got)
		}
	}
	source := []byte("jobs: [ # actionlint:ignore syntax-check")
	_, findings := Parse(source)
	got := filterInlineSuppressions(source, findings, nil)
	if len(got) != 2 || got[1].Kind != "inline-suppression" || !strings.Contains(got[1].Message, "reason") {
		t.Fatalf("missing reason was accepted: %+v", got)
	}
	cfg, err := ParseConfig([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	source = []byte("jobs: [ # actionlint:ignore syntax-check -- reviewed")
	_, findings = Parse(source)
	if got := filterInlineSuppressions(source, findings, cfg.Policy.DisallowSuppressions); len(got) != 2 || got[1].Kind != "disallow-suppressions" {
		t.Fatalf("malformed YAML bypassed suppression policy: %+v", got)
	}
	source = []byte("name: test # actionlint:ignore syntax-check -- reviewed\njobs: [")
	_, findings = Parse(source)
	if got := filterInlineSuppressions(source, findings, nil); len(got) != 1 || got[0] != findings[0] {
		t.Fatalf("unrelated declaration suppressed syntax error: %+v", got)
	}
}

func TestInlineSuppressionScriptTextIsNotDirective(t *testing.T) {
	source := []byte("run: |\n  text = '''\n  # actionlint:ignore-next-line expression -- sample text\n  ${{ github.event.issue.title }}\n  '''\n")
	finding := &Error{Line: 4, Kind: "expression"}
	got := filterInlineSuppressions(source, []*Error{finding}, nil)
	if len(got) != 1 || got[0] != finding {
		t.Fatalf("script text changed findings: %+v", got)
	}
}

func TestInlineSuppressionBlockExpression(t *testing.T) {
	source := "on: issues\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: | # actionlint:ignore expression -- reviewed input\n          echo '${{ github.event.issue.title }}'\n"
	if got := lintCachePolicy(t, source, ""); len(got) != 0 {
		t.Fatal(got)
	}
	got := lintCachePolicy(t, source, "policy: {disallow-suppressions: true}")
	if len(got) != 2 || got[0].Kind != "disallow-suppressions" || got[1].Kind != "expression" {
		t.Fatalf("prohibited block suppression: %+v", got)
	}
}
