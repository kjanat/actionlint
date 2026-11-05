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
		// GitHub accepts these; the style and numeric-values rules report them.
		{"empty on", "on:\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", nil},
		{"empty on sequence", "on: []\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", nil},
		{"empty on mapping", "on: {}\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", nil},
		{"empty run", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run:\n", nil},
		{"job without steps", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n", nil},
		{"empty steps", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    steps: []\n", nil},
		{"fractional max-parallel", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: .5\n    steps:\n      - run: echo ok\n", nil},
		// The expression rule reports the boolean; the parser accepts the form.
		{"boolean timeout expression", "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    timeout-minutes: ${{ false }}\n    steps:\n      - run: echo ok\n", nil},
		{"missing on", "jobs:\n  check:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo ok\n", []Code{CodeMissingKey}},
		{"missing runs-on", "on: push\njobs:\n  check:\n    steps:\n      - run: echo ok\n", []Code{CodeMissingKey}},
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

// GitHub converts a fractional integer literal by truncating toward zero.
func TestParseIntTruncatesFractions(t *testing.T) {
	for _, tc := range []struct {
		literal, truncated string
		value              int
	}{
		{"1.5", "1.5", 1},
		{".5", ".5", 0},
		{"-1.5", "-1.5", -1},
		{"2", "", 2},
		{"0x10", "", 16},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			source := "on: push\njobs:\n  check:\n    runs-on: ubuntu-latest\n    strategy:\n      max-parallel: " + tc.literal + "\n    steps:\n      - run: echo ok\n"
			workflow, findings := Parse([]byte(source))
			if len(findings) != 0 {
				t.Fatal(findings)
			}
			got := workflow.Jobs["check"].Strategy.MaxParallel
			if got.Value != tc.value || got.TruncatedFrom != tc.truncated {
				t.Fatalf("max-parallel = %d from %q, want %d from %q", got.Value, got.TruncatedFrom, tc.value, tc.truncated)
			}
		})
	}
}
