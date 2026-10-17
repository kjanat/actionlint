package engine

import (
	"strings"
	"testing"
)

func TestInsertedUnknownMatrixAxisKeepsKnownProperties(t *testing.T) {
	for _, insertion := range []string{
		`{'${{ vars.AXIS }}': [ubuntu-latest], known: [one]}`,
		`{'${{ insert }}': {'${{ vars.AXIS }}': [ubuntu-latest]}, known: [one]}`,
	} {
		source := "on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    strategy:\n      matrix:\n        '${{ insert }}': " + insertion + "\n    steps:\n      - run: echo '${{ matrix.os }} ${{ matrix.known }}'\n"
		if findings := lintCredentialsExpressionTest(t, source); len(findings) != 0 {
			t.Fatalf("dynamic insertion rejected: %v", findings)
		}
	}
}

func TestActionMetadataDuplicatesAcrossInsertions(t *testing.T) {
	for _, value := range []string{
		`{FOO: one, '${{ insert }}': {FOO: two}}`,
		`{'${{ insert }}': {foo: one}, FOO: two}`,
		`{FOO: one, '${{ insert }}': {'${{ insert }}': {foo: two}}}`,
		`{FOO: one, '${{ insert }}': "${{ fromJSON('{\"foo\":\"two\"}') }}"}`,
		`{FOO: one, '${{ insert }}': {"${{ 'foo' }}": two}}`,
	} {
		source := "name: test\ndescription: test\nruns:\n  using: composite\n  steps:\n    - shell: bash\n      run: echo ok\n      env: " + value + "\n"
		result, err := runTestAnalysis(t.Context(), Request{Sources: []SourceUnit{{Path: "action.yml", Content: []byte(source)}}})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, finding := range result.Diagnostics {
			if strings.Contains(finding.Message, "duplicate key") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("mapping %s: want one duplicate, got %+v", value, result.Diagnostics)
		}
	}
}
