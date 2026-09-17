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
				{"unsupported", "expression", "", "inline-suppression"},
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
	const invalid = "# actionlint:ignore expression -- reviewed"
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
		{"standalone before scalar", "name: !!str\n  # actionlint:ignore-next-line expression -- reviewed\n  title\n", 3},
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
