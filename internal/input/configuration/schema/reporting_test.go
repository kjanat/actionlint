package schema

import "testing"

func TestReportingDefaultsSchema(t *testing.T) {
	t.Parallel()
	schema := compile(t)
	for _, tc := range []struct {
		name, source string
		valid        bool
	}{
		{"valid", `{"output":{"formats":["github","pretty"],"log-level":"debug","summary":false,"network-stats":false,"quiet":false},"lint":{"experimental":true}}`, true},
		{"invalid format", `{"output":{"formats":["bad"]}}`, false},
		{"invalid level", `{"output":{"log-level":"verbose"}}`, false},
		{"invalid summary", `{"output":{"summary":"false"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := schema.Validate([]byte(tc.source))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}
