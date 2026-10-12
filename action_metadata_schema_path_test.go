package actionlint

import (
	"strings"
	"testing"
)

func TestActionSchemaMemberPath(t *testing.T) {
	for _, tc := range []struct{ parent, key, want string }{
		{"", "runs", "runs"},
		{"runs", "pre-if", "runs.pre-if"},
		{"", ".leading", `['.leading']`},
		{"outputs", "0", `outputs['0']`},
		{"outputs", "a\nb", "outputs['a\nb']"},
		{"outputs", "a b", `outputs['a b']`},
		{"outputs", "a'b", `outputs['a''b']`},
		{"outputs", "", `outputs['']`},
	} {
		if got := actionSchemaMemberPath(tc.parent, tc.key); got != tc.want {
			t.Errorf("member path(%q, %q) = %q, want %q", tc.parent, tc.key, got, tc.want)
		}
	}
}

func TestActionSchemaQuotedMemberPaths(t *testing.T) {
	for _, tc := range []struct{ name, body, path string }{
		{"dotted-output", "outputs:\n  x.y:\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x.y'].value`},
		{"bracket-output", "outputs:\n  'x[0]':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x[0]'].value`},
		{"escaped-output", "outputs:\n  'x\"y\\z':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x"y\z'].value`},
		{"dotted-input", "runs:\n  using: composite\n  steps:\n    - uses: owner/action@v1\n      with:\n        a.b: [invalid]\n", `runs.steps[0].with['a.b']`},
		{"normal-output", "outputs:\n  normal_name:\n    value: [invalid]\nruns: {using: composite, steps: []}\n", "outputs.normal_name.value"},
		{"control-output", "outputs:\n  \"x\\ny\\tz\\e\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x\ny\tz\x1b'].value`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\n"+tc.body)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			for _, finding := range result.Diagnostics {
				if strings.Contains(finding.Message, "expected a scalar string at \""+tc.path+"\" in action metadata") {
					return
				}
			}
			t.Fatalf("missing concrete path %s in %+v", tc.path, result.Diagnostics)
		})
	}
}
