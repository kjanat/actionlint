package module

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kjanat/wazero/api"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func TestTemplateEffectNegotiation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, contract string
		version        int
		valid          bool
	}{
		{"legacy", "", 1, true},
		{"typed", scriptengine.TemplateEffectsContract, 2, true},
		{"missing version", scriptengine.TemplateEffectsContract, 1, false},
		{"unknown contract", "example:effects@9.0.0", 2, false},
		{"missing contract", "", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := pooledEngine(t)
			metadata := engine.Metadata()
			metadata.TemplateContract = tc.contract
			metadata.Capabilities.ScriptShells = []string{"python"}
			metadata.Interfaces = map[string][]Interface{"check": {{Version: tc.version, Export: "check"}}}
			data, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.negotiate(data); (err == nil) != tc.valid {
				t.Fatalf("negotiate: %v", err)
			}
			if tc.valid && engine.ScriptDescriptor().TemplateContract != tc.contract {
				t.Fatal("public descriptor lost negotiated contract")
			}
		})
	}
}

func TestPartialOutcomeRejectsUnknownTemplateEffects(t *testing.T) {
	t.Parallel()
	engine := pooledEngine(t)
	engine.metadata.TemplateContract = scriptengine.TemplateEffectsContract
	module, err := engine.instantiate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	good := scriptengine.Diagnostic{Code: "ANY", Message: "arbitrary wording", Location: sourcetext.Position{Line: 1, Column: 1}, TemplateEffect: scriptengine.TemplateEffectExportedName}
	bad := good
	bad.TemplateEffect = "future-effect"
	payload, err := json.Marshal(response{OK: false, Diagnostics: []scriptengine.Diagnostic{good, bad}, Errors: []string{"partial"}})
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := module.ExportedFunction("alloc").Call(t.Context(), uint64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	pointer := uint32(allocation[0])
	if !module.Memory().Write(pointer, payload) {
		t.Fatal("write response")
	}
	read := false
	engine.idle = []api.Module{responseModule{Module: module, pointer: pointer, length: uint32(len(payload)), read: &read}}
	findings, err := engine.Check(t.Context(), scriptengine.Request{Source: "fixture"})
	if err == nil || len(findings) != 1 || !reflect.DeepEqual(findings[0], good) {
		t.Fatalf("findings=%+v error=%v", findings, err)
	}
}

func TestCoreCoordinatesUseDecodedSourceRows(t *testing.T) {
	t.Parallel()
	engine := pooledEngine(t)
	module, err := engine.instantiate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(response{OK: true, Diagnostics: []scriptengine.Diagnostic{{Code: "ANY", Location: sourcetext.Position{Line: 2, Column: 1}, EndLocation: sourcetext.Position{Line: 2, Column: 3}}}})
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := module.ExportedFunction("alloc").Call(t.Context(), uint64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	pointer := uint32(allocation[0])
	if !module.Memory().Write(pointer, payload) {
		t.Fatal("write response")
	}
	read := false
	engine.idle = []api.Module{responseModule{Module: module, pointer: pointer, length: uint32(len(payload)), read: &read}}
	findings, err := engine.Check(t.Context(), scriptengine.Request{Source: "é\rxy"})
	if err != nil || len(findings) != 1 || findings[0].Location != (sourcetext.Position{Line: 1, Column: 3}) || findings[0].EndLocation != (sourcetext.Position{Line: 1, Column: 5}) {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
}
