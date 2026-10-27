package declaration

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

func TestDeclarationProjectionPreservesKeySourceAndOrigin(t *testing.T) {
	const source = `on: push
env:
  plain: environment value
  "${{ 'constant' }}": constant value
  "${{ inputs.runtime_key }}": dynamic value
  "${{ fromJSON('[]') }}": invalid value
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        "${{ 'axis' }}": [linux]
        "${{ inputs.runtime_axis }}": [windows]
        "${{ fromJSON('[]') }}": [invalid]
    steps:
      - uses: actions/checkout@v4
        with:
          plain: input value
          "${{ 'constant' }}": constant input
          "${{ inputs.runtime_key }}": dynamic input
          "${{ fromJSON('[]') }}": invalid input
          "${{ insert }}":
            inserted: inserted input
`
	workflow, errors := workflowparser.Parse([]byte(source))
	if len(errors) != 0 {
		t.Fatalf("parse source: %v", errors)
	}
	bound := Publish(source, workflow, false, "").Bind("declaration-source")
	mappings, _ := bound.Declarations()
	byScope := map[string]analyzer.Mapping{}
	for _, mapping := range mappings {
		byScope[mapping.Scope] = mapping
	}
	assertEntry := func(scope, sourceKey, name string, state analyzer.KeyState, evaluated bool) analyzer.Entry {
		t.Helper()
		declaration, ok := byScope[scope]
		if !ok {
			t.Fatalf("mapping %q missing; have scopes %v", scope, keys(byScope))
		}
		for _, entry := range declaration.Entries {
			if entry.Source == sourceKey {
				if entry.Name != name || entry.State != state || entry.Evaluated != evaluated {
					t.Fatalf("%s entry %#v; want name %q state %q evaluated %v", scope, entry, name, state, evaluated)
				}
				return entry
			}
		}
		t.Fatalf("%s has no source key %q; entries=%#v", scope, sourceKey, declaration.Entries)
		return analyzer.Entry{}
	}

	for _, scope := range []string{"env"} {
		assertEntry(scope, "plain", "plain", analyzer.KnownKey, false)
		assertEntry(scope, "${{ 'constant' }}", "constant", analyzer.KnownKey, false)
		dynamic := assertEntry(scope, "${{ inputs.runtime_key }}", "", analyzer.UnknownKey, false)
		assertEntry(scope, "${{ fromJSON('[]') }}", "", analyzer.InvalidKey, false)
		if byScope[scope].Complete || dynamic.Start.Column != sourceColumn(source, `"${{ inputs.runtime_key }}"`) {
			t.Fatalf("%s runtime key projection lost incompleteness or key position: %#v", scope, dynamic)
		}
	}
	withScope := `jobs["build"].steps[0].with`
	plain := assertEntry(withScope, "plain", "plain", analyzer.KnownKey, false)
	if plain.Start.Column != sourceColumn(source, "plain: input value") || plain.Start.Column == sourceColumn(source, "input value") {
		t.Fatalf("action input start used value column, got %d", plain.Start.Column)
	}
	assertEntry(withScope, "${{ 'constant' }}", "constant", analyzer.KnownKey, false)
	assertEntry(withScope, "${{ inputs.runtime_key }}", "", analyzer.UnknownKey, false)
	assertEntry(withScope, "${{ fromJSON('[]') }}", "", analyzer.InvalidKey, false)
	assertEntry(withScope, "inserted", "inserted", analyzer.KnownKey, false)
	matrix := assertEntry(`jobs["build"].strategy.matrix`, "${{ inputs.runtime_axis }}", "", analyzer.UnknownKey, false)
	if matrix.Start.Column != sourceColumn(source, `"${{ inputs.runtime_axis }}"`) {
		t.Fatalf("matrix key start = %d; want source key column %d", matrix.Start.Column, sourceColumn(source, `"${{ inputs.runtime_axis }}"`))
	}
	assertEntry(`jobs["build"].strategy.matrix`, "${{ fromJSON('[]') }}", "", analyzer.InvalidKey, false)
}

func sourceColumn(source, fragment string) int {
	for line := range strings.SplitSeq(source, "\n") {
		if index := strings.Index(line, fragment); index >= 0 {
			return index + 1
		}
	}
	return 0
}

func keys[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}
