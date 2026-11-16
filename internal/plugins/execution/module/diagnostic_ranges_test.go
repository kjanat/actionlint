package module

import (
	"encoding/json"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking/inputs"
	"github.com/kjanat/wazero/api"
)

func TestCheckValidatesDiagnosticRanges(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		start, end inputs.ScriptPosition
		valid      bool
	}{
		{"missing start", inputs.ScriptPosition{}, inputs.ScriptPosition{}, false},
		{"zero start column", inputs.ScriptPosition{Row: 1}, inputs.ScriptPosition{}, false},
		{"negative start", inputs.ScriptPosition{Row: -1, Column: 1}, inputs.ScriptPosition{}, false},
		{"omitted end", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{}, true},
		{"partial end row", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: 2}, false},
		{"partial end column", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Column: 3}, false},
		{"negative end", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: -1, Column: 1}, false},
		{"earlier row", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: 1, Column: 9}, false},
		{"earlier column", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: 2, Column: 2}, false},
		{"equal", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: 2, Column: 3}, true},
		{"later row", inputs.ScriptPosition{Row: 2, Column: 3}, inputs.ScriptPosition{Row: 3, Column: 1}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := pooledEngine(t)
			module, err := engine.instantiate(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			good := inputs.ScriptDiagnostic{Code: "GOOD", Location: inputs.ScriptPosition{Row: 1, Column: 1}}
			candidate := inputs.ScriptDiagnostic{Code: "CANDIDATE", Location: test.start, EndLocation: test.end}
			payload, err := json.Marshal(response{OK: true, Diagnostics: []inputs.ScriptDiagnostic{good, candidate, good}})
			if err != nil {
				t.Fatal(err)
			}
			allocation, err := module.ExportedFunction("alloc").Call(t.Context(), uint64(len(payload)))
			if err != nil {
				t.Fatal(err)
			}
			pointer := uint32(allocation[0])
			if !module.Memory().Write(pointer, payload) {
				t.Fatal("write fixture response")
			}
			read := false
			engine.idle = []api.Module{responseModule{Module: module, pointer: pointer, length: uint32(len(payload)), read: &read}}
			findings, err := engine.Check(t.Context(), inputs.ScriptRequest{Source: "fixture"})
			if test.valid {
				if err != nil || len(findings) != 3 || !reflect.DeepEqual(findings[1], candidate) {
					t.Fatalf("valid range: findings=%+v err=%v", findings, err)
				}
			} else if err == nil || len(findings) != 2 || !reflect.DeepEqual(findings[0], good) || !reflect.DeepEqual(findings[1], good) {
				t.Fatalf("invalid range: findings=%+v err=%v", findings, err)
			}
		})
	}
}
