package module

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kjanat/wazero/api"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

func TestCheckValidatesDiagnosticRanges(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		start, end sourcetext.Position
		valid      bool
	}{
		{"missing start", sourcetext.Position{}, sourcetext.Position{}, false},
		{"zero start column", sourcetext.Position{Line: 1}, sourcetext.Position{}, false},
		{"negative start", sourcetext.Position{Line: -1, Column: 1}, sourcetext.Position{}, false},
		{"omitted end", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{}, true},
		{"partial end row", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 2}, false},
		{"partial end column", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Column: 3}, false},
		{"negative end", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: -1, Column: 1}, false},
		{"earlier row", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 1, Column: 9}, false},
		{"earlier column", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 2, Column: 2}, false},
		{"equal", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 2, Column: 3}, true},
		{"later row", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 3, Column: 1}, true},
		{"past line end", sourcetext.Position{Line: 2, Column: 9}, sourcetext.Position{}, false},
		{"past snippet", sourcetext.Position{Line: 2, Column: 3}, sourcetext.Position{Line: 4, Column: 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine := pooledEngine(t)
			module, err := engine.instantiate(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			good := scriptengine.Diagnostic{Code: "GOOD", Location: sourcetext.Position{Line: 1, Column: 1}}
			candidate := scriptengine.Diagnostic{Code: "CANDIDATE", Location: test.start, EndLocation: test.end}
			payload, err := json.Marshal(response{OK: true, Diagnostics: []scriptengine.Diagnostic{good, candidate, good}})
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
			findings, err := engine.Check(t.Context(), scriptengine.Request{Source: "fixture\nfixture\n"})
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
