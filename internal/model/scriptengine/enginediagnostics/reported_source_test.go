package enginediagnostics

import "testing"

func TestValidReportedSource(t *testing.T) {
	t.Parallel()
	for name, valid := range map[string]bool{
		"lib.sh": true, "scripts/lib.sh": true, "": false, ".": false, "../lib.sh": false, "a/../../b": false,
		"/etc/passwd": false, "scripts\\lib.sh": false, "C:lib.sh": false, "a//b": false, "a\x00b": false,
	} {
		if ValidReportedSource(name) != valid {
			t.Errorf("ValidReportedSource(%q) = %t, want %t", name, !valid, valid)
		}
	}
}
