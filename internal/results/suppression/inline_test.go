package suppression

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"

	"github.com/google/go-cmp/cmp"
)

func TestInlineSuppressionSpellings(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{
		"actionlint:ignore expression,shellcheck",
		"actionlint:ignore expression shellcheck",
		"actionlint:ignore\texpression\tshellcheck",
		"actionlint:ignore expression, shellcheck expression",
		"actionlint-ignore expression,shellcheck",
		"actionlint-ignore expression shellcheck",
		"actionlint-ignore\texpression,\tshellcheck",
		"ignore expression,shellcheck",
		"ignore expression shellcheck",
		"ignore\texpression,\tshellcheck",
		"ignore[expression,shellcheck]",
		"ignore[expression shellcheck]",
		"ignore[ expression,\tshellcheck expression ]",
	} {
		for _, next := range []string{"next: value", "next: ["} {
			for _, ending := range []string{"\n", "\r\n"} {
				t.Run(declaration+"/"+next+"/"+ending, func(t *testing.T) {
					t.Parallel()
					source := strings.ReplaceAll("run: | # "+declaration+" -- reviewed -- input\n  echo first\n  echo second\n"+next+"\n", "\n", ending)
					directives, findings := Parse([]byte(source), []string{"expression", "shellcheck"}, rule.Names{})
					want := []Directive{{
						Position: sourcepos.Position{Line: 1, Column: 8}, First: 1, Last: 3,
						Rules: []string{"expression", "shellcheck"}, Reason: "reviewed -- input",
					}}
					if len(findings) != 0 {
						t.Fatalf("unexpected grammar diagnostics: %+v", findings)
					}
					if diff := cmp.Diff(want, directives); diff != "" {
						t.Fatal(diff)
					}
					result, _ := Evaluate(directives, nil, rule.Names{})
					for _, rule := range []string{"expression", "shellcheck"} {
						if !result.Suppresses(rule, 2, 2) || result.Suppresses(rule, 4, 4) {
							t.Fatalf("wrong declaration extent for %s", rule)
						}
					}
				})
			}
		}
	}
}

func TestInlineSuppressionNextLineSpellings(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"actionlint:ignore-next-line", "actionlint-ignore-next-line", "ignore-next-line"} {
		for _, selectors := range []string{"expression,shellcheck", "expression shellcheck", "expression,\tshellcheck"} {
			t.Run(command+"/"+selectors, func(t *testing.T) {
				t.Parallel()
				source := "# " + command + " " + selectors + "\nrun: |\n  echo ok\nnext: value\n"
				directives, findings := Parse([]byte(source), []string{"expression", "shellcheck"}, rule.Names{})
				want := []Directive{{Position: sourcepos.Position{Line: 1, Column: 1}, First: 2, Last: 3, Rules: []string{"expression", "shellcheck"}}}
				if len(findings) != 0 {
					t.Fatalf("unexpected grammar diagnostics: %+v", findings)
				}
				if diff := cmp.Diff(want, directives); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestInlineSuppressionSpellingIsolation(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{"actionlint:ignore expression", "actionlint-ignore expression", "ignore[expression]", "ignore expression", "ignore-next-line expression"} {
		for _, next := range []string{"next: value", "next: ["} {
			source := "name: '# " + declaration + "' # ignored by another tool\nrun: |\n  # " + declaration + "\n  echo ok\n" + next + "\n"
			directives, findings := Parse([]byte(source), []string{"expression"}, rule.Names{})
			if len(directives) != 0 || len(findings) != 0 {
				t.Fatalf("text became a directive: %q: %+v, %+v", source, directives, findings)
			}
		}
	}
}

func TestIgnoreProse(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"ignore", "ignore docs as they are built elsewhere.", "ignore unknown files"} {
		for _, next := range []string{"next: value", "next: ["} {
			source := "paths-ignore: # " + text + "\n  - '**/*.md'\n" + next + "\n"
			directives, findings := Parse([]byte(source), []string{"expression", "shellcheck"}, rule.Names{})
			if len(directives) != 0 || len(findings) != 0 {
				t.Fatalf("prose %q produced directives=%v findings=%v", text, directives, findings)
			}
		}
	}
}

func TestUnknownSuppressionRuleMessage(t *testing.T) {
	t.Parallel()
	_, findings := Parse([]byte("run: echo ok # actionlint:ignore unknown\n"), []string{"expression", "shellcheck"}, rule.Names{})
	const want = "unknown inline suppression rule \"unknown\". Run actionlint rules to list available rules."
	if len(findings) != 1 || findings[0].Message != want {
		t.Fatalf("findings=%v, want one finding with message %q", findings, want)
	}
}

func TestMisspelledSuppressionRuleMessage(t *testing.T) {
	t.Parallel()
	_, findings := Parse([]byte("run: echo ok # actionlint:ignore shellchekc\n"), []string{"expression", "shellcheck"}, rule.Names{})
	const want = "unknown inline suppression rule \"shellchekc\". Did you mean \"shellcheck\"? Run actionlint rules to list available rules."
	if len(findings) != 1 || findings[0].Message != want {
		t.Fatalf("findings=%v, want one finding with message %q", findings, want)
	}
}

func TestPlainIgnoreNextLineRecovery(t *testing.T) {
	t.Parallel()
	for _, next := range []string{"next: value", "next: ["} {
		for _, ending := range []string{"\n", "\r\n"} {
			source := strings.ReplaceAll("#ignore-next-line\texpression\nrun: |\n  echo ok\n"+next+"\n", "\n", ending)
			directives, findings := Parse([]byte(source), []string{"expression"}, rule.Names{})
			if len(findings) != 0 || len(directives) != 1 {
				t.Fatalf("plain next-line directive: %+v, %+v", directives, findings)
			}
			result, _ := Evaluate(directives, nil, rule.Names{})
			if !result.Suppresses("expression", 2, 3) || result.Suppresses("expression", 4, 4) {
				t.Fatalf("wrong declaration extent: %+v", directives)
			}
		}
	}
}

func TestInlineSuppressionMalformedSpellings(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{
		"actionlint:ignore expression,",
		"actionlint:ignore expression, ,shellcheck",
		"actionlint-ignore ,expression",
		"actionlint-ignore expression,,shellcheck",
		"actionlint-ignore",
		"actionlint-ignore unknown",
		"actionlint-ignore-next-line expression",
		"ignore expression,,shellcheck",
		"ignore-next-line expression",
		"ignore[]",
		"ignore[ ]",
		"ignore[expression,]",
		"ignore[expression, ,shellcheck]",
		"ignore[unknown]",
		"ignore[expression",
		"ignore[expression]]",
		"ignore[expression][shellcheck]",
		"ignore[expression] trailing",
		"ignore[expression] --",
	} {
		for _, next := range []string{"next: value", "next: ["} {
			t.Run(declaration+"/"+next, func(t *testing.T) {
				t.Parallel()
				source := "run: echo ok # " + declaration + "\n" + next + "\n"
				directives, findings := Parse([]byte(source), []string{"expression", "shellcheck"}, rule.Names{})
				if len(directives) != 0 || len(findings) != 1 || findings[0].Rule != "inline-suppression" {
					t.Fatalf("malformed directive accepted: %+v, %+v", directives, findings)
				}
			})
		}
	}
}

func TestInlineSuppressionStages(t *testing.T) {
	t.Parallel()
	const source = `name: one
  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused
run-name: |
  two # actionlint:ignore cache-operation, cache-operation -- reviewed -- intentionally unused
`
	comments := collectInlineSuppressionComments([]byte(source))
	if len(comments) != 1 || comments[0].pos != (sourcepos.Position{Line: 2, Column: 7}) || comments[0].standalone {
		t.Fatalf("want only the real closing comment at 2:7, got %+v", comments)
	}
	directive, err := parseInlineSuppression(comments[0], []string{"cache-operation"}, rule.Names{})
	if err != nil {
		t.Fatal(err)
	}
	want := Directive{
		Position: sourcepos.Position{Line: 2, Column: 7}, First: 2, Last: 2,
		Rules: []string{"cache-operation"}, Reason: "reviewed -- intentionally unused",
	}
	if diff := cmp.Diff(want, directive, cmp.AllowUnexported(Directive{})); diff != "" {
		t.Fatal(diff)
	}
}
