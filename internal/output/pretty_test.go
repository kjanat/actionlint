package output

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/outputformat"
	"actionlint.kjanat.dev/v2/internal/model/rule"
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
	renderer, err := New(outputformat.Pretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := renderer.SetWidth(40); err != nil {
		t.Fatal(err)
	}
	result := RenderedReport{Findings: []results.PublishedDiagnostic{publishedFinding(diagnostic, nil)}}
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

func TestPrettyMarksDependencyWithoutRepeatingPolicyAdvice(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	finding := publishedFinding(results.Diagnostic{Rule: "script-interpolation", Origin: results.DependencyOrigin, Message: "Use an environment variable.", Path: "https://example.test/action.yml", NonBlocking: true}, nil)
	if err := printPretty(&out, finding, 120, false, false, "lint.rules.correctness.script-interpolation"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "does not affect the exit code") {
		t.Fatalf("dependency finding repeats policy advice:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "error[dependency:script-interpolation]") {
		t.Fatalf("dependency origin is missing:\n%s", out.String())
	}
}

func TestPrettyRemoteLocationsAndURLsStayIntact(t *testing.T) {
	t.Parallel()
	const address = "https://github.com/pypa/gh-action-pypi-publish/blob/7245906fb61fa33a13554f9d5fb856fe61758086/action.yml"
	for _, hyperlinks := range []bool{false, true} {
		var out strings.Builder
		f := publishedFinding(results.Diagnostic{
			Path: address, Start: sourcetext.Position{Line: 168, Column: 11},
			Message: "See " + address + ".", DocumentationURL: address,
			Related: []results.Related{{Path: address, Start: sourcetext.Position{Line: 10, Column: 2}}},
		}, nil)
		if err := printPretty(&out, f, 100, hyperlinks, false, ""); err != nil {
			t.Fatal(err)
		}
		osc := regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`)
		plain := osc.ReplaceAllString(out.String(), "")
		for _, want := range []string{
			"  --> " + address + " (line 168, column 11)\n",
			"    --> " + address + " (line 10, column 2)\n",
			address + ".\n", address + "\n",
		} {
			if !strings.Contains(plain, want) {
				t.Errorf("hyperlinks=%v: missing intact URL %q in:\n%s", hyperlinks, want, plain)
			}
		}
		if hyperlinks {
			links := regexp.MustCompile(`\x1b\]8;;([^\x1b]+)\x1b\\`).FindAllStringSubmatch(out.String(), -1)
			// Message, location, documentation, related location.
			want := []string{address, address + "#L168", address, address + "#L10"}
			if len(links) != len(want) {
				t.Fatalf("got %d OSC 8 links, want %d:\n%q", len(links), len(want), out.String())
			}
			for i, match := range links {
				if match[1] != want[i] {
					t.Errorf("OSC 8 destination %d: got %q, want %q", i, match[1], want[i])
				}
			}
		} else if strings.Contains(out.String(), "\x1b") {
			t.Fatal("plain output contains terminal controls")
		}
	}
}

func TestPrettyRemoteLocationLinksToLine(t *testing.T) {
	t.Parallel()
	const address = "https://github.com/actions/checkout/blob/0123456789abcdef0123456789abcdef01234567/action.yml"
	for _, tc := range []struct {
		name     string
		path     string
		position sourcetext.Position
		want     string
	}{
		{
			name: "line", path: address, position: sourcetext.Position{Line: 42, Column: 7},
			want: "  --> \x1b]8;;" + address + "#L42\x1b\\" + address + "\x1b]8;;\x1b\\ (line 42, column 7)\n",
		},
		{
			name: "existing fragment", path: address + "#readme", position: sourcetext.Position{Line: 3, Column: 1},
			want: "  --> \x1b]8;;" + address + "#L3\x1b\\" + address + "#readme\x1b]8;;\x1b\\ (line 3, column 1)\n",
		},
		{
			name: "no line", path: address, position: sourcetext.Position{},
			want: "  --> \x1b]8;;" + address + "\x1b\\" + address + "\x1b]8;;\x1b\\ (line 0, column 0)\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var linked, plain strings.Builder
			writePrettyLocation(&linked, "  --> ", tc.path, tc.position, 20, true)
			if linked.String() != tc.want {
				t.Fatalf("got  %q\nwant %q", linked.String(), tc.want)
			}
			writePrettyLocation(&plain, "  --> ", tc.path, tc.position, 20, false)
			visible := regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\`).ReplaceAllString(linked.String(), "")
			if visible != plain.String() {
				t.Fatalf("link changed visible text: got %q, want %q", visible, plain.String())
			}
		})
	}
}

func TestPrettyHyperlinksSanitizeControls(t *testing.T) {
	t.Parallel()
	const message = "Read https://example.com/safe\x1b]8;;https://attacker.example\a and https://example.com/next."
	var out strings.Builder
	f := publishedFinding(results.Diagnostic{Message: message}, nil)
	if err := printPretty(&out, f, 40, true, false, ""); err != nil {
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
	r, err := New(outputformat.Pretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := results.Diagnostic{
		Severity: "warning", Rule: "expression-reference", Message: "unknown variable",
		Path: "ci.yml", Start: sourcetext.Position{Line: 1, Column: 3}, End: sourcetext.Position{Line: 1, Column: 6},
		Help: "Use a declared value.", Related: []results.Related{{Message: "declaration", Path: "other.yml", Start: sourcetext.Position{Line: 4, Column: 2}}},
	}
	var out strings.Builder
	if err := r.Render(&out, RenderedReport{Findings: []results.PublishedDiagnostic{publishedFinding(d, []byte("  bad"))}}); err != nil {
		t.Fatal(err)
	}
	want := "warning[expression-reference]\n  unknown variable\n  --> ci.yml:1:3\n  |\n1 |   bad\n  |   ^^^\n  = help: Use a declared value.\n  = related: declaration\n    --> other.yml:4:2\n\n"
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", &out, want)
	}
	if err := r.Render(failingRenderWriter{}, RenderedReport{Findings: []results.PublishedDiagnostic{publishedFinding(d, nil)}}); err == nil {
		t.Fatal("lost write failure")
	}
}

func TestPrettyNativeCodeAndSpecificAdvice(t *testing.T) {
	t.Parallel()
	descriptor := rule.Descriptor{Name: "shellcheck", Help: "Configure this plugin.", Explanation: "Checks shell snippets.", DocumentationURL: "https://example.com/rule"}
	d := results.Diagnostic{
		Rule: descriptor.Name, Code: "SC2035", Severity: "info", Message: "Use ./*glob* or -- *glob*",
		Path: "ci.yml", Start: sourcetext.Position{Line: 440, Column: 36},
		Help: descriptor.Help, Explanation: descriptor.Explanation,
		DocumentationURL: descriptor.DocumentationURL,
	}
	r, err := New(outputformat.Pretty, "", false, testVersion, []rule.Descriptor{descriptor})
	if err != nil {
		t.Fatal(err)
	}
	for _, specific := range []bool{false, true} {
		if specific {
			d.Help = "Quote this variable."
		}
		var out strings.Builder
		published := publishedFinding(d, nil)
		result := RenderedReport{Findings: []results.PublishedDiagnostic{published}, Diagnostics: []results.Diagnostic{d}}
		if err := r.Render(&out, result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "info[shellcheck] SC2035\n  Use ./*glob* or -- *glob*") ||
			strings.Contains(out.String(), descriptor.Help) || strings.Contains(out.String(), descriptor.Explanation) ||
			strings.Contains(out.String(), descriptor.DocumentationURL) ||
			strings.Contains(out.String(), "Quote this variable.") != specific {
			t.Fatalf("specific=%t: %s", specific, &out)
		}
		if !reflect.DeepEqual(result.Findings[0], published) || !reflect.DeepEqual(result.Diagnostics[0], d) {
			t.Fatal("pretty output changed diagnostic metadata")
		}
	}
}

func TestPrettyRuleConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rules    []rule.Descriptor
		result   []rule.Descriptor
		finding  string
		wantPath string
	}{
		{
			name: "built-in rule", rules: []rule.Descriptor{{Name: "case-insensitive-conditions", Category: "suspicious"}},
			finding: "case-insensitive-conditions", wantPath: "lint.rules.suspicious.case-insensitive-conditions",
		},
		{
			name: "plugin rule from analysis", result: []rule.Descriptor{{Name: "custom-check", Category: "security", Provider: "custom-plugin"}},
			finding: "custom-check", wantPath: "lint.rules.security.custom-check",
		},
		{
			name: "registered rule without category", rules: []rule.Descriptor{{Name: "example"}}, finding: "example",
		},
		{name: "unknown rule", finding: "unregistered"},
		{name: "diagnostic without rule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			renderer, err := New(outputformat.Pretty, "", false, testVersion, tc.rules)
			if err != nil {
				t.Fatal(err)
			}
			diagnostic := results.Diagnostic{Rule: tc.finding, Code: "specific-code", Message: "message"}
			published := publishedFinding(diagnostic, nil)
			result := RenderedReport{Rules: tc.result, Findings: []results.PublishedDiagnostic{published}}
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
			if !reflect.DeepEqual(result.Findings[0], published) {
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
		f := publishedFinding(results.Diagnostic{Start: sourcetext.Position{Line: 1, Column: tc.column}, End: sourcetext.Position{Line: 1, Column: tc.end}}, []byte(tc.line))
		line, start, length := prettySource(f, 30)
		if line != tc.want || start != tc.start || length != tc.length {
			t.Errorf("%q: got %q %d %d, want %q %d %d", tc.line, line, start, length, tc.want, tc.start, tc.length)
		}
	}
	var out strings.Builder
	f := publishedFinding(results.Diagnostic{
		Rule: "long-rule", Message: strings.Repeat("界", 100) + "\nnext paragraph",
		Path: strings.Repeat("path/", 30), Help: "escape\x1b[31m must not execute",
	}, nil)
	if err := printPretty(&out, f, 40, false, false, "lint.rules.suspicious."+strings.Repeat("long-rule", 10)); err != nil {
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
	d := results.Diagnostic{Severity: "warning", Message: "message", Path: "file.yml", Start: sourcetext.Position{Line: 8, Column: 4}}
	renderer, err := New(outputformat.Pretty, "", false, testVersion, nil)
	if err != nil {
		t.Fatal(err)
	}
	renderer.SetColor(true)
	var out strings.Builder
	if err := renderer.Render(&out, RenderedReport{Findings: []results.PublishedDiagnostic{publishedFinding(d, nil)}}); err != nil {
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
		Path: "ci.yml", Start: sourcetext.Position{Line: 1, Column: 3}, End: sourcetext.Position{Line: 1, Column: 6},
	}
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	for _, format := range []outputformat.Format{outputformat.Pretty, outputformat.Text, outputformat.Oneline} {
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
				result := RenderedReport{Findings: []results.PublishedDiagnostic{publishedFinding(d, []byte("  bad"))}}
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
