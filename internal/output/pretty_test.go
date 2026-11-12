package output

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	sourcepos "actionlint.kjanat.dev/v2/internal/input/source"
	"actionlint.kjanat.dev/v2/internal/results"
	"github.com/mattn/go-runewidth"
)

func TestPrettyWrappedHyperlinks(t *testing.T) {
	t.Parallel()
	target := "https://example.com/" + strings.Repeat("long-path/", 9) + "rule?q=hello%20world#details"
	help := "https://example.com/help_(bash)?a=1&b=2"
	docs := "https://example.com/docs"
	diagnostic := results.Diagnostic{
		Rule: "example", Message: "See " + target + ".",
		Help: "Read [the help](" + help + ").", DocumentationURL: docs,
	}
	renderer, err := New(FormatPretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.SetWidth(40); err != nil {
		t.Fatal(err)
	}
	result := Result{Findings: []Finding{FindingOf(diagnostic, nil)}}
	var plain, linked strings.Builder
	if err := renderer.Render(&plain, result); err != nil {
		t.Fatal(err)
	}
	renderer.SetHyperlinks(true)
	if err := renderer.Render(&linked, result); err != nil {
		t.Fatal(err)
	}
	osc := regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)
	if got := osc.ReplaceAllString(linked.String(), ""); got != plain.String() {
		t.Fatalf("links changed visible wrapping:\n%q\nwant:\n%q", got, plain.String())
	}
	links := regexp.MustCompile(`\x1b\]8;;([^\x1b]+)\x1b\\([^\x1b]*)\x1b\]8;;\x1b\\`).FindAllStringSubmatch(linked.String(), -1)
	labels := map[string]string{}
	for _, link := range links {
		if link[1] != target && link[1] != help && link[1] != docs {
			t.Fatalf("incorrect link destination: %q", link[1])
		}
		if strings.ContainsAny(link[2], "\r\n") || strings.HasPrefix(link[2], "  = ") {
			t.Fatalf("link includes a line break or gutter: %q", link[2])
		}
		labels[link[1]] += link[2]
	}
	for _, target := range []string{target, help, docs} {
		if labels[target] != target {
			t.Errorf("visible URL parts=%q, want %q", labels[target], target)
		}
	}
}

func TestPrettyHyperlinksSanitizeControls(t *testing.T) {
	t.Parallel()
	const message = "Read https://example.com/safe\x1b]8;;https://attacker.example\a and https://example.com/next."
	var out strings.Builder
	f := FindingOf(results.Diagnostic{Message: message}, nil)
	if err := f.printPretty(&out, 40, true, false, ""); err != nil {
		t.Fatal(err)
	}
	osc := regexp.MustCompile(`\x1b\]8;;[^\x1b\a]*\x1b\\`)
	plain := osc.ReplaceAllString(out.String(), "")
	if strings.ContainsAny(plain, "\x1b\a") {
		t.Fatalf("untrusted control reached terminal: %q", out.String())
	}
	if !strings.Contains(out.String(), "\x1b]8;;https://example.com/safe\x1b\\") ||
		!strings.Contains(out.String(), "\x1b]8;;https://example.com/next\x1b\\") {
		t.Fatalf("URL detection did not stop at sanitized controls and punctuation: %q", out.String())
	}
}

func TestPrettyDiagnostic(t *testing.T) {
	t.Parallel()
	r, err := New(FormatPretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := results.Diagnostic{
		Severity: "warning", Rule: "expression-reference", Message: "unknown variable",
		Path: "ci.yml", Start: sourcepos.Position{Line: 1, Column: 3}, End: sourcepos.Position{Line: 1, Column: 6},
		Help: "Use a declared value.", Related: []results.Related{{Message: "declaration", Path: "other.yml", Start: sourcepos.Position{Line: 4, Column: 2}}},
	}
	var out strings.Builder
	if err := r.Render(&out, Result{Findings: []Finding{FindingOf(d, []byte("  bad"))}}); err != nil {
		t.Fatal(err)
	}
	want := "warning[expression-reference]\n  unknown variable\n  --> ci.yml:1:3\n  |\n1 |   bad\n  |   ^^^\n  = help: Use a declared value.\n  = related: declaration\n    --> other.yml:4:2\n\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", &out, want)
	}
	if err := r.Render(failingRenderWriter{}, Result{Findings: []Finding{FindingOf(d, nil)}}); err == nil {
		t.Fatal("lost write failure")
	}
}

func TestPrettyNativeCodeAndSpecificAdvice(t *testing.T) {
	t.Parallel()
	rule := Rule{Name: "shellcheck", Help: "Configure this plugin.", Explanation: "Checks shell snippets.", DocumentationURL: "https://example.com/rule"}
	d := results.Diagnostic{
		Rule: rule.Name, Code: "SC2035", Severity: "info", Message: "Use ./*glob* or -- *glob*",
		Path: "ci.yml", Start: sourcepos.Position{Line: 440, Column: 36},
		Help: rule.Help, Explanation: rule.Explanation,
		DocumentationURL: rule.DocumentationURL,
	}
	r, err := New(FormatPretty, "", false, testVersion, []Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	for _, specific := range []bool{false, true} {
		if specific {
			d.Help = "Quote this variable."
		}
		var out strings.Builder
		result := Result{Findings: []Finding{FindingOf(d, nil)}, Diagnostics: []results.Diagnostic{d}}
		if err := r.Render(&out, result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "info[shellcheck] SC2035\n  Use ./*glob* or -- *glob*") ||
			strings.Contains(out.String(), rule.Help) || strings.Contains(out.String(), rule.Explanation) ||
			strings.Contains(out.String(), rule.DocumentationURL) ||
			strings.Contains(out.String(), "Quote this variable.") != specific {
			t.Fatalf("specific=%t: %s", specific, &out)
		}
		if !reflect.DeepEqual(result.Findings[0].Diagnostic, d) || !reflect.DeepEqual(result.Diagnostics[0], d) {
			t.Fatal("pretty output changed diagnostic metadata")
		}
	}
}

func TestPrettyRuleConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rules    []Rule
		result   []Rule
		finding  string
		wantPath string
	}{
		{
			name: "built-in rule", rules: []Rule{{Name: "case-insensitive-conditions", Category: "suspicious"}},
			finding: "case-insensitive-conditions", wantPath: "lint.rules.suspicious.case-insensitive-conditions",
		},
		{
			name: "plugin rule from analysis", result: []Rule{{Name: "custom-check", Category: "security", Provider: "custom-plugin"}},
			finding: "custom-check", wantPath: "lint.rules.security.custom-check",
		},
		{
			name: "registered rule without category", rules: []Rule{{Name: "example"}}, finding: "example",
		},
		{name: "unknown rule", finding: "unregistered"},
		{name: "diagnostic without rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			renderer, err := New(FormatPretty, "", false, testVersion, tc.rules)
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := results.Diagnostic{Rule: tc.finding, Code: "specific-code", Message: "message"}
			result := Result{Rules: tc.result, Findings: []Finding{FindingOf(diagnostic, nil)}}
			var out strings.Builder
			if err := renderer.Render(&out, result); err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.wantPath != "" {
				want = "  = config: " + tc.wantPath + "\n"
			}
			var got strings.Builder
			for line := range strings.SplitSeq(out.String(), "\n") {
				if strings.HasPrefix(line, "  = config:") {
					got.WriteString(line)
					got.WriteByte('\n')
				}
			}
			if got.String() != want {
				t.Fatalf("configuration line=%q, want %q; output:\n%s", got.String(), want, &out)
			}
			if !reflect.DeepEqual(result.Findings[0].Diagnostic, diagnostic) {
				t.Fatal("rendering changed diagnostic metadata")
			}
		})
	}
}

func TestPrettyWidthAndCoordinates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line          string
		column, end   int
		want          string
		start, length int
	}{
		{"\t界e\u0301!", 3, 4, "    界e\u0301!", 6, 1},
		{strings.Repeat("a", 80) + "bad", 81, 84, "..." + strings.Repeat("a", 10) + "bad", 13, 3},
		{"abc", 4, 4, "abc", 3, 1},
		{"abc", 8, 8, "abc", -1, 1},
	} {
		f := FindingOf(results.Diagnostic{Start: sourcepos.Position{Line: 1, Column: tc.column}, End: sourcepos.Position{Line: 1, Column: tc.end}}, []byte(tc.line))
		line, start, length := f.prettySource(tc.line, 30)
		if line != tc.want || start != tc.start || length != tc.length {
			t.Errorf("%q: got %q %d %d, want %q %d %d", tc.line, line, start, length, tc.want, tc.start, tc.length)
		}
	}
	var out strings.Builder
	f := FindingOf(results.Diagnostic{
		Rule: "long-rule", Message: strings.Repeat("界", 100) + "\nnext paragraph",
		Path: strings.Repeat("path/", 30), Help: "escape\x1b[31m must not execute",
	}, nil)
	if err := f.printPretty(&out, 40, false, false, "lint.rules.suspicious."+strings.Repeat("long-rule", 10)); err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(out.String(), "\n") {
		if runewidth.StringWidth(line) > 40 {
			t.Errorf("line exceeds width: %q", line)
		}
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("untrusted terminal escape")
	}
}

func TestPrettyColorAndUnavailableSource(t *testing.T) {
	t.Parallel()
	d := results.Diagnostic{Severity: "warning", Message: "message", Path: "file.yml", Start: sourcepos.Position{Line: 8, Column: 4}}
	renderer, err := New(FormatPretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	renderer.SetColor(true)
	var out strings.Builder
	if err := renderer.Render(&out, Result{Findings: []Finding{FindingOf(d, nil)}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[1;33mwarning\x1b[22;0m\n") {
		t.Fatalf("missing color: %q", out.String())
	}
	if strings.Contains(out.String(), " |") {
		t.Fatal("invented unavailable source")
	}
}

func TestRendererColorMode(t *testing.T) {
	t.Parallel()
	d := results.Diagnostic{
		Severity: "warning", Rule: "expression-reference", Message: "unknown variable",
		Path: "ci.yml", Start: sourcepos.Position{Line: 1, Column: 3}, End: sourcepos.Position{Line: 1, Column: 6},
	}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, format := range []Format{FormatPretty, FormatText, FormatOneline} {
		for _, colored := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/color=%t", format, colored), func(t *testing.T) {
				t.Parallel()
				plain, err := New(format, "", false, testVersion, nil)
				if err != nil {
					t.Fatal(err)
				}
				renderer, err := New(format, "", false, testVersion, nil)
				if err != nil {
					t.Fatal(err)
				}
				renderer.SetColor(colored)
				result := Result{Findings: []Finding{FindingOf(d, []byte("  bad"))}}
				var want, got strings.Builder
				if err := plain.Render(&want, result); err != nil {
					t.Fatal(err)
				}
				if err := renderer.Render(&got, result); err != nil {
					t.Fatal(err)
				}
				if ansi.MatchString(want.String()) {
					t.Fatalf("renderer without a color mode wrote color: %q", want.String())
				}
				if ansi.MatchString(got.String()) != colored {
					t.Fatalf("color=%t: %q", colored, got.String())
				}
				if stripped := ansi.ReplaceAllString(got.String(), ""); stripped != want.String() {
					t.Fatalf("color changed the text:\n%q\nwant:\n%q", stripped, want.String())
				}
			})
		}
	}
}
