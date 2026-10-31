package action_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/action"
)

func TestDefinitionOwnsCallSiteFacts(t *testing.T) {
	metadata, err := action.Parse("action.yml", []byte("name: test\ninputs:\n  token: {required: true}\noutputs:\n  result: {description: result}\nruns: {using: node24, main: index.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	definition := action.NewDefinition(metadata)
	metadata.Inputs["token"].Name = "changed"
	delete(metadata.Inputs, "token")
	delete(metadata.Outputs, "result")
	metadata.Runs.Using = "changed"
	names := definition.InputNames()
	names[0] = "changed"
	outputs := definition.OutputNames()
	outputs[0] = "changed"
	input, exists := definition.Input("token")
	if !definition.Present() || !exists || input.Name != "token" || !input.Required || definition.Using() != "node24" || definition.InputNames()[0] != "token" || definition.OutputNames()[0] != "result" {
		t.Fatal("definition retained writable manifest storage")
	}
}
