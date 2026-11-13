package action_test

import (
	"slices"
	"testing"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
)

func TestActionDiagnosticCodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"runtime and file", "name: example\ndescription: example\nruns: {using: node20, main: missing.js}\n", []string{"missing-file", "runtime-deprecated"}},
		{"replaced runtime", "name: example\ndescription: example\nruns: {using: node16}\n", []string{"missing-field", "runtime-deprecated"}},
		{"unsupported runtime", "name: example\ndescription: example\nruns: {using: invalid}\n", []string{"runtime-unsupported"}},
		{"composite shell", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok}]}\n", []string{"missing-shell"}},
		{"context", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: 'echo ${{ secrets.TOKEN }}', shell: bash}]}\n", []string{"expression-context"}},
		{"branding", "name: example\ndescription: example\nbranding: {icon: bad-icon, color: invalid}\nruns: {using: composite, steps: []}\n", []string{"invalid-branding-color", "invalid-branding-icon"}},
		{"schema type", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok, shell: bash, continue-on-error: []}]}\n", []string{"invalid-field-type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, err := actionmeta.Parse("action.yml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			meta.Directory = t.TempDir()
			var codes []string
			for _, finding := range actioncheck.Standalone(meta, actionmeta.NewReferencedFiles(map[string]actionmeta.FileAvailability{"missing.js": actionmeta.FileMissing})) {
				if finding.Rule != "action" || finding.Code == "" || finding.Code == "action" {
					t.Fatalf("nonspecific action diagnostic: %+v", finding)
				}
				codes = append(codes, finding.Code)
			}
			slices.Sort(codes)
			if !slices.Equal(codes, tc.want) {
				t.Fatalf("codes = %v, want %v", codes, tc.want)
			}
		})
	}
}

func TestMetadataCoercionProblemsUseTheirOwnRule(t *testing.T) {
	t.Parallel()
	const expression = "${{ format('{0}', startsWith(true, 'TR')) }}"
	for _, tc := range []struct{ name, source string }{
		{"output value", "name: example\ndescription: example\noutputs:\n  value:\n    description: value\n    value: " + expression + "\nruns: {using: composite, steps: []}\n"},
		{"docker environment value", "name: example\ndescription: example\nruns:\n  using: docker\n  image: docker://alpine:3\n  env:\n    VALUE: " + expression + "\n"},
		{"docker environment key", "name: example\ndescription: example\nruns:\n  using: docker\n  image: docker://alpine:3\n  env:\n    \"" + expression + "\": value\n"},
		{"input default", "name: example\ndescription: example\ninputs:\n  value:\n    description: value\n    default: " + expression + "\nruns: {using: composite, steps: []}\n"},
		{"composite step", "name: example\ndescription: example\nruns:\n  using: composite\n  steps:\n    - name: " + expression + "\n      run: echo ok\n      shell: bash\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, err := actionmeta.Parse("action.yml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			inspection := actionmeta.InspectDefinition([]byte(tc.source))
			if _, valid := inspection.Definition(); !valid || len(inspection.Problems()) != 0 {
				t.Fatalf("supported coercion invalidated the definition: %+v", inspection.Problems())
			}
			if findings := actioncheck.Standalone(meta, actionmeta.ReferencedFiles{}); len(findings) != 0 {
				t.Fatalf("supported coercion became an action error: %+v", findings)
			}
			findings := actioncheck.StandaloneWithAdvisories(meta, actionmeta.ReferencedFiles{})
			if len(findings) != 1 || findings[0].Rule != "coercion" || findings[0].Code != "coercion" || findings[0].Start.Line == 0 || findings[0].Start.Column == 0 {
				t.Fatalf("coercion problem lost its rule or source range: %+v", findings)
			}
		})
	}
}
