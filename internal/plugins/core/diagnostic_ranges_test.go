package core

import (
	"encoding/json"
	"testing"

	"actionlint.kjanat.dev/v2/analyzer"
	"github.com/kjanat/wazero/api"
)

func TestCheckValidatesDiagnosticRanges(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end analyzer.ScriptPosition
		valid      bool
	}{
		{"missing start", analyzer.ScriptPosition{}, analyzer.ScriptPosition{}, false},
		{"zero start column", analyzer.ScriptPosition{Row: 1}, analyzer.ScriptPosition{}, false},
		{"negative start", analyzer.ScriptPosition{Row: -1, Column: 1}, analyzer.ScriptPosition{}, false},
		{"omitted end", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{}, true},
		{"partial end row", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: 2}, false},
		{"partial end column", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Column: 3}, false},
		{"negative end", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: -1, Column: 1}, false},
		{"earlier row", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: 1, Column: 9}, false},
		{"earlier column", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: 2, Column: 2}, false},
		{"equal", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: 2, Column: 3}, true},
		{"later row", analyzer.ScriptPosition{Row: 2, Column: 3}, analyzer.ScriptPosition{Row: 3, Column: 1}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := pooledEngine(t)
			module, err := engine.instantiate(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			good := analyzer.ScriptDiagnostic{Code: "GOOD", Location: analyzer.ScriptPosition{Row: 1, Column: 1}}
			candidate := analyzer.ScriptDiagnostic{Code: "CANDIDATE", Location: test.start, EndLocation: test.end}
			payload, err := json.Marshal(response{OK: true, Diagnostics: []analyzer.ScriptDiagnostic{good, candidate, good}})
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
			findings, err := engine.Check(t.Context(), analyzer.ScriptRequest{Source: "fixture"})
			if test.valid {
				if err != nil || len(findings) != 3 || findings[1] != candidate {
					t.Fatalf("valid range: findings=%+v err=%v", findings, err)
				}
			} else if err == nil || len(findings) != 2 || findings[0] != good || findings[1] != good {
				t.Fatalf("invalid range: findings=%+v err=%v", findings, err)
			}
		})
	}
}
