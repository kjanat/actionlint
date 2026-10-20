package expressions_test

import (
	"fmt"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestInsertedUnknownMatrixAxisKeepsKnownProperties(t *testing.T) {
	for depth := range 3 {
		t.Run(fmt.Sprintf("depth=%d", depth), func(t *testing.T) {
			matrix := `{'${{ vars.AXIS }}': [ubuntu-latest], count: [1]}`
			for range depth {
				matrix = "{'${{ insert }}': " + matrix + "}"
			}
			source := "on: push\njobs:\n  test:\n    runs-on: ${{ matrix.os }}\n    strategy:\n      matrix: " + matrix + "\n    steps: [{run: echo ok}]\n"
			if errs := lintCredentialsExpressionTest(t, source); len(errs) != 0 {
				t.Fatalf("unknown axis rejected: %v", errs)
			}
		})
	}
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
		result, err := application.Analyze(t.Context(), application.Options{}, []workspace.Source{{Path: "action.yml", Content: []byte(source)}})
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
