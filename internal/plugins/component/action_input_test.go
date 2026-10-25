package component

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

func TestInputOnlyScriptMetadata(t *testing.T) {
	p, err := Load(t.Context(), "testdata/plugins/action-input.wasm", Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	descriptor := p.ScriptDescriptor()
	if len(descriptor.Shells) != 0 || len(descriptor.ScriptInputs) != 2 {
		t.Fatalf("input-only descriptor: %+v", descriptor)
	}
	descriptor.ScriptInputs[0].Input = "changed"
	if p.ScriptDescriptor().ScriptInputs[0].Input != "script" {
		t.Fatal("descriptor input selectors alias caller memory")
	}
	if err := verifyScriptMetadata(nil, p.scriptDescriptor); err == nil {
		t.Fatal("input-only component accepted without static selectors")
	}
	data, err := os.ReadFile("testdata/plugins/action-input.wasm")
	if err != nil {
		t.Fatal(err)
	}
	_, metadata, err := artifact.Sniff(data)
	if err != nil {
		t.Fatal(err)
	}
	var declared map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &declared); err != nil {
		t.Fatal(err)
	}
	valid := analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}
	for _, inputs := range [][]analyzer.ScriptInput{
		nil,
		{{Action: "Actions/github-script", Input: "script", Language: "javascript"}},
		{{Action: "actions/github-script@v7", Input: "script", Language: "javascript"}},
		{valid, valid},
		{valid, {Action: valid.Action, Input: valid.Input, Language: "python"}},
	} {
		capabilities, err := json.Marshal(struct {
			Shells []string               `json:"script_shells"`
			Inputs []analyzer.ScriptInput `json:"script_inputs"`
		}{Shells: []string{}, Inputs: inputs})
		if err != nil {
			t.Fatal(err)
		}
		declared["capabilities"] = capabilities
		changed, err := json.Marshal(declared)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyScriptMetadata(changed, p.scriptDescriptor); err == nil {
			t.Fatalf("invalid input selectors accepted: %+v", inputs)
		}
	}
}
