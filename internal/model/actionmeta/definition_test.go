package actionmeta_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls"
)

func TestDefinitionOwnsCallSiteFacts(t *testing.T) {
	t.Parallel()
	metadata, err := actionmeta.ParseActionMetadata("action.yml", []byte("name: test\ninputs:\n  token: {required: true}\noutputs:\n  result: {description: result}\nruns: {using: node24, main: index.js}\n"))
	if err != nil {
		t.Fatal(err)
	}
	definition := actionmeta.NewDefinition(metadata)
	metadata.Inputs["token"] = calls.Input{Name: "changed"}
	delete(metadata.Inputs, "token")
	delete(metadata.Outputs, "result")
	metadata.Runs.Using = "changed"
	names := definition.InputNames()
	names[0] = "changed"
	outputs := definition.OutputIDs()
	outputs[0] = "changed"
	input, exists := definition.Input("token")
	if !definition.Present() || !exists || input.Name != "token" || !input.Required || definition.Using() != "node24" || definition.InputNames()[0] != "token" || definition.OutputIDs()[0] != "result" {
		t.Fatal("definition retained writable manifest storage")
	}
}
