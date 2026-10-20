package engine

import (
	"strings"
	"testing"
)

func TestActionSchemaQuotedMemberPaths(t *testing.T) {
	for _, tc := range []struct{ name, body, path string }{
		{"dotted-output", "outputs:\n  x.y:\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x.y'].value`},
		{"bracket-output", "outputs:\n  'x[0]':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x[0]'].value`},
		{"escaped-output", "outputs:\n  'x\"y\\z':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x"y\\z'].value`},
		{"dotted-input", "runs:\n  using: composite\n  steps:\n    - uses: owner/action@v1\n      with:\n        a.b: [invalid]\n", `runs.steps[0].with['a.b']`},
		{"normal-output", "outputs:\n  normal_name:\n    value: [invalid]\nruns: {using: composite, steps: []}\n", "outputs.normal_name.value"},
		{"control-output", "outputs:\n  \"x\\ny\\tz\\e\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x\ny\tz\x1b'].value`},
		{"literal-backslash-n", "outputs:\n  'x\\ny':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x\\ny'].value`},
		{"newline", "outputs:\n  \"x\\ny\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['x\ny'].value`},
		{"bidi", "outputs:\n  \"p\\u202Eq\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['p\u202eq'].value`},
		{"zero-width-space", "outputs:\n  \"p\\u200Bq\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['p\u200bq'].value`},
		{"bom", "outputs:\n  \"p\\uFEFFq\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['p\ufeffq'].value`},
		{"line-separator", "outputs:\n  \"p\\u2028q\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['p\u2028q'].value`},
		{"paragraph-separator", "outputs:\n  \"p\\u2029q\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['p\u2029q'].value`},
		{"printable-unicode", "outputs:\n  'café日本':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['café日本'].value`},
		{"single-quote", "outputs:\n  \"a'b\":\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['a''b'].value`},
		{"wildcard-key", "outputs:\n  '*':\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs['*'].value`},
		{"case-preserved", "outputs:\n  MixedCase:\n    value: [invalid]\nruns: {using: composite, steps: []}\n", `outputs.MixedCase.value`},
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

func TestActionSchemaExpressionPaths(t *testing.T) {
	for _, tc := range []struct{ name, body, message string }{
		{
			"loose-output-value",
			"outputs:\n  \"x\\ny\":\n    value: ${{ secrets.X }}\nruns: {using: composite, steps: []}\n",
			`context "secrets" is not available at "outputs['x\ny'].value" in action metadata`,
		},
		{
			"sequence-value",
			"runs: {using: docker, image: Dockerfile, args: ['ok', '${{ secrets.X }}']}\n",
			`context "secrets" is not available at "runs.args[1]" in action metadata`,
		},
		{
			"mapping-key",
			"runs:\n  using: composite\n  steps:\n    - run: echo ok\n      shell: bash\n    - uses: owner/action@v1\n      with:\n        '${{ secrets.X }}': ok\n",
			`context "secrets" is not available in keys at "runs.steps[1].with" in action metadata`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := executableFixture(t)
			writeShellcheckFixture(t, root, ".github/actionlint.yaml", "tools: {shellcheck: false, ruff: false}\n")
			writeShellcheckFixture(t, root, "inner/action.yml", "name: inner\ndescription: test\n"+tc.body)
			result := compositeAnalysis(t, root, "- uses: ./inner", AnalysisOptions{})
			for _, finding := range result.Diagnostics {
				if strings.Contains(finding.Message, tc.message) {
					return
				}
			}
			t.Fatalf("missing message %q in %+v", tc.message, result.Diagnostics)
		})
	}
}
