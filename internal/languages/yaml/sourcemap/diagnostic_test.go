package sourcemap

import "testing"

func TestDiagnosticLinePolicies(t *testing.T) {
	for _, tc := range []struct {
		name     string
		source   string
		line     int
		yamlLine string
		yamlOK   bool
		rawLine  string
		rawOK    bool
	}{
		{name: "BOM", source: "\ufefffirst\nsecond", line: 1, yamlLine: "first", yamlOK: true, rawLine: "\ufefffirst", rawOK: true},
		{name: "CRLF", source: "first\r\nsecond", line: 2, yamlLine: "second", yamlOK: true, rawLine: "second", rawOK: true},
		{name: "CR", source: "first\rsecond", line: 2, yamlLine: "second", yamlOK: true},
		{name: "NEL", source: "first\u0085second", line: 2, yamlLine: "second", yamlOK: true},
		{name: "LS", source: "first\u2028second", line: 2, yamlLine: "second", yamlOK: true},
		{name: "PS", source: "first\u2029second", line: 2, yamlLine: "second", yamlOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotYAML, okYAML := DiagnosticLine([]byte(tc.source), tc.line, false)
			if gotYAML != tc.yamlLine || okYAML != tc.yamlOK {
				t.Fatalf("YAML DiagnosticLine(%q, %d) = (%q, %t), want (%q, %t)", tc.source, tc.line, gotYAML, okYAML, tc.yamlLine, tc.yamlOK)
			}
			gotRaw, okRaw := DiagnosticLine([]byte(tc.source), tc.line, true)
			if gotRaw != tc.rawLine || okRaw != tc.rawOK {
				t.Fatalf("raw DiagnosticLine(%q, %d) = (%q, %t), want (%q, %t)", tc.source, tc.line, gotRaw, okRaw, tc.rawLine, tc.rawOK)
			}
		})
	}
}
