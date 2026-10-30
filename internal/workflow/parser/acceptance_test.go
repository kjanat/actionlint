package parser

import (
	"slices"
	"testing"
)

// GitHub's workflow reader rejects these documents; ADR-0017 records the
// service verdicts. Each must produce exactly its acceptance code.
func TestParserAcceptanceCodes(t *testing.T) {
	const body = "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n"
	for _, tc := range []struct {
		name, source string
		want         []Code
	}{
		{"plain", body, nil},
		{"leading document marker", "---\n" + body, nil},
		{"document end marker", body + "...\n", nil},
		{"byte order mark", "\xef\xbb\xbf" + body, []Code{CodeBOM}},
		{"second document", body + "---\nfoo: bar\n", []Code{CodeMultipleDocuments}},
		{"bare tag on run", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: ! y\n", []Code{CodeUnknownTag}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := Parse([]byte(tc.source))
			var got []Code
			for _, finding := range findings {
				got = append(got, finding.Code)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("codes = %q, want %q: %v", got, tc.want, findings)
			}
		})
	}
}
