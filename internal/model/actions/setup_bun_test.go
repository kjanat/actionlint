package action

import (
	"os"
	"testing"

	reference "actionlint.kjanat.dev/v2/internal/development/tests/conformance/expressions"
)

func TestSetupBunMetadata(t *testing.T) {
	t.Parallel()
	// https://github.com/oven-sh/setup-bun/blob/0c5077e51419868618aeaa5fe8019c62421857d6/action.yml
	source, err := os.ReadFile("testdata/metadata/setup-bun.yaml")
	if err != nil {
		t.Fatal(err)
	}
	inspection := InspectDefinition(source)
	definition, ok := inspection.Definition()
	if !ok {
		t.Fatalf("setup-bun metadata rejected: %+v", inspection.Problems())
	}
	if input, ok := definition.Input("no-cache"); !ok || input.Required {
		t.Fatalf("optional no-cache input lost: %+v, %t", input, ok)
	}
	if definition.Using() != "node24" {
		t.Fatalf("runtime = %q, want node24", definition.Using())
	}
	reference.CheckAction(t, string(source), true)
}
