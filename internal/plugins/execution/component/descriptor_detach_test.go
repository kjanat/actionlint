package component

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	rule "actionlint.kjanat.dev/v2/internal/checking/rules"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

func TestAnalyzerDescriptorDetachesNestedMetadata(t *testing.T) {
	t.Parallel()
	p := &Plugin{analyzerDescriptor: &inputs.Descriptor{
		Name: "example", Aliases: []rule.Alias{{Name: "original"}},
		InvocationDefinitions: []commands.Definition{{ID: "runtime", Options: []commands.Option{{Choices: []string{"original"}}}}},
	}}
	descriptor := p.Descriptor()
	descriptor.Aliases[0].Name = "changed"
	descriptor.InvocationDefinitions[0].Options[0].Choices[0] = "changed"
	again := p.Descriptor()
	if again.Aliases[0].Name != "original" || again.InvocationDefinitions[0].Options[0].Choices[0] != "original" {
		t.Fatalf("descriptor mutation changed plugin metadata: %+v", again)
	}
}
