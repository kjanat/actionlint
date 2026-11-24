package actioncheck_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/rules/correctness/actioncheck"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
)

func TestActionDiagnosticCodes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"runtime and file", "name: example\ndescription: example\nruns: {using: node20, main: missing.js}\n", []string{"action-runtime/runtime-deprecated", "missing-file/missing-file"}},
		{"replaced runtime", "name: example\ndescription: example\nruns: {using: node16}\n", []string{"action-runtime/runtime-deprecated", "syntax-check/missing-field"}},
		{"unsupported runtime", "name: example\ndescription: example\nruns: {using: invalid}\n", []string{"action-runtime/runtime-unsupported"}},
		{"composite shell", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok}]}\n", []string{"shell-name/missing-shell"}},
		{"context", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: 'echo ${{ secrets.TOKEN }}', shell: bash}]}\n", []string{"context-availability/expression-context"}},
		{"branding", "name: example\ndescription: example\nbranding: {icon: bad-icon, color: invalid}\nruns: {using: composite, steps: []}\n", []string{"action-branding/invalid-branding-color", "action-branding/invalid-branding-icon"}},
		{"schema type", "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok, shell: bash, continue-on-error: []}]}\n", []string{"syntax-check/invalid-field-type"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta, err := actionmeta.ParseActionMetadata("action.yml", []byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			meta.Directory = t.TempDir()
			var codes []string
			for _, finding := range actioncheck.Standalone(meta, actionmeta.NewReferencedFiles(map[string]actionmeta.FileAvailability{"missing.js": actionmeta.FileMissing})) {
				if finding.Rule == "action" || finding.Code == "" || finding.Code == "action" {
					t.Fatalf("nonspecific action diagnostic: %+v", finding)
				}
				codes = append(codes, finding.Rule+"/"+finding.Code)
			}
			slices.Sort(codes)
			if !slices.Equal(codes, tc.want) {
				t.Fatalf("codes = %v, want %v", codes, tc.want)
			}
		})
	}
}

func TestMetadataCoercionReportsCoercionRule(t *testing.T) {
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
			meta, err := actionmeta.ParseActionMetadata("action.yml", []byte(tc.source))
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
			findings := actioncheck.StandaloneWithAdvisories(meta, actionmeta.ReferencedFiles{}, actionmeta.LifecycleContext{})
			if len(findings) != 1 || findings[0].Rule != "coercion" || findings[0].Code != "coercion" || findings[0].Start.Line == 0 || findings[0].Start.Column == 0 {
				t.Fatalf("coercion problem lost its rule or source range: %+v", findings)
			}
		})
	}
}

func TestFindingsShareManifestContent(t *testing.T) {
	t.Parallel()
	const source = "name: example\ndescription: example\nruns: {using: composite, steps: [{run: echo ok}, {run: echo ok, shell: bash, continue-on-error: []}]}\n"
	parsed, err := actionmeta.ParseActionMetadata("action.yml", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	findings := actioncheck.Standalone(parsed, actionmeta.NewReferencedFiles(nil))
	var codes []string
	for _, item := range findings {
		codes = append(codes, item.Code)
		if string(item.Source.Content) != source || &item.Source.Content[0] != &findings[0].Source.Content[0] {
			t.Fatalf("finding does not share the manifest buffer: %+v", item)
		}
	}
	slices.Sort(codes)
	if !slices.Equal(codes, []string{"invalid-field-type", "missing-shell"}) {
		t.Fatalf("fixture must report a step finding and a schema finding: %v", codes)
	}
	handBuilt := &actionmeta.Metadata{Name: "example", Description: "example", Runs: actionmeta.Runs{Using: "composite", Steps: []*actionmeta.CompositeStep{{}}}}
	findings = actioncheck.Standalone(handBuilt, actionmeta.NewReferencedFiles(nil))
	if len(findings) == 0 {
		t.Fatal("hand-built fixture reported no step finding")
	}
	for _, item := range findings {
		if item.Source.Content != nil {
			t.Fatalf("hand-built metadata finding has content: %+v", item)
		}
	}
}
