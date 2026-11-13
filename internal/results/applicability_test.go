package results

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

func TestApplicabilityWireContract(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		wire string
		want Applicability
	}{
		{`{}`, FixSuggested},
		{`{"applicability":""}`, FixSuggested},
		{`{"applicability":"suggested"}`, FixSuggested},
		{`{"applicability":"safe"}`, FixSafe},
		{`{"applicability":"unsafe"}`, FixUnsafe},
	} {
		for _, decode := range []func([]byte, *Fix) error{
			func(data []byte, fix *Fix) error { return json.Unmarshal(data, fix) },
			func(data []byte, fix *Fix) error { return jsonv2.Unmarshal(data, fix) },
		} {
			var fix Fix
			if err := decode([]byte(tc.wire), &fix); err != nil || fix.Applicability != tc.want {
				t.Fatalf("%s: got %v, %v", tc.wire, fix.Applicability, err)
			}
			data, err := json.Marshal(fix)
			if err != nil {
				t.Fatal(err)
			}
			var roundtrip Fix
			if err := decode(data, &roundtrip); err != nil || roundtrip.Applicability != tc.want {
				t.Fatalf("roundtrip %s: %v, %v", data, roundtrip.Applicability, err)
			}
			if fix.Applicability.Automatic() != (tc.want == FixSafe) {
				t.Fatalf("incorrect automatic application for %s", tc.wire)
			}
		}
	}
	for _, wire := range []string{`{"applicability":"automatic"}`, `{"applicability":1}`, `{"applicability":true}`} {
		var fix Fix
		if err := json.Unmarshal([]byte(wire), &fix); err == nil {
			t.Fatalf("accepted invalid fix: %s", wire)
		}
		if err := jsonv2.Unmarshal([]byte(wire), &fix); err == nil {
			t.Fatalf("JSON v2 accepted invalid fix: %s", wire)
		}
	}
}

func TestInvalidApplicabilityCannotSerialize(t *testing.T) {
	t.Parallel()
	if _, err := json.Marshal(Fix{Applicability: 255}); err == nil {
		t.Fatal("serialized an invalid applicability")
	}
}
