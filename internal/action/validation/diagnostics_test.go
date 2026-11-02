package validation_test

import (
	"slices"
	"testing"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
)

func TestActionDiagnosticCodes(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"runtime and file", "name: example\ndescription: example\nruns: {using: node20, main: missing.js}\n", []string{"missing-file", "runtime-deprecated"}},
		{"removed runtime", "name: example\ndescription: example\nruns: {using: node16}\n", []string{"missing-field", "runtime-removed"}},
		{"unsupported runtime", "name: example\ndescription: example\nruns: {using: invalid}\n", []string{"runtime-unsupported"}},
		{"composite shell", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok}]}\n", []string{"missing-shell"}},
		{"context", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: 'echo ${{ secrets.TOKEN }}', shell: bash}]}\n", []string{"expression-context"}},
		{"branding", "name: example\ndescription: example\nbranding: {icon: bad-icon, color: invalid}\nruns: {using: composite, steps: []}\n", []string{"invalid-branding-color", "invalid-branding-icon"}},
		{"schema type", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok, shell: bash, continue-on-error: []}]}\n", []string{"invalid-field-type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := actionmeta.Parse("action.yml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			meta.Directory = t.TempDir()
			var codes []string
			for _, finding := range actioncheck.Standalone(meta) {
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
