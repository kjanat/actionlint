package services

import (
	"encoding/json"
	"testing"
)

func TestServiceValidationAndJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, version string
		valid         bool
	}{
		{"unknown", "", true},
		{"github.com", "", true},
		{"github-data-residency", "", true},
		{"ghes", "", true},
		{"ghes", "3.19", true},
		{"ghes", "3.19.2", true},
		{"ghes", "3", false},
		{"ghes", "3.1.2.3", false},
		{"ghes", "3.x", false},
		{"github.com", "3.19", false},
		{"invalid", "", false},
	} {
		t.Run(tc.kind+"/"+tc.version, func(t *testing.T) {
			t.Parallel()
			s, err := ParseService(tc.kind, tc.version)
			if (err == nil) != tc.valid {
				t.Fatalf("Parse error=%v", err)
			}
			if err != nil {
				return
			}
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Service
			if err := json.Unmarshal(data, &decoded); err != nil || decoded != s {
				t.Fatalf("roundtrip=%+v error=%v", decoded, err)
			}
		})
	}
}
