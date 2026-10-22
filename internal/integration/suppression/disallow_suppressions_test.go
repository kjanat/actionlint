package suppression_test

import (
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
	"github.com/google/go-cmp/cmp"
)

func kindCounts(kinds []string) map[string]int {
	counts := map[string]int{}
	for _, kind := range kinds {
		counts[kind]++
	}
	return counts
}

func TestDisallowSuppressionsInteractions(t *testing.T) {
	const body = `
jobs:
  test:
` + testutil.WorkflowSteps
	for _, tc := range []struct {
		name, declaration, config string
		kinds                     []string
	}{
		{"unused", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "true", []string{"disallow-suppressions"}},
		{"unused violation only", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "{report: violation}", nil},
		{"optional reason", "cache-mode: write # actionlint:ignore cache-write-untrusted", "true", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"cannot exempt restriction", "cache-mode: write # actionlint:ignore disallow-suppressions, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"duplicates", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"unselected still suppressed", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-operation]}", []string{"disallow-suppressions"}},
		{"selected restored", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-write-untrusted]}", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"detached", `# actionlint:ignore-next-line cache-write-untrusted -- reviewed

cache-mode: write`, "true", []string{"cache-write-untrusted"}},
		{"quoted", "name: '# actionlint:ignore cache-write-untrusted -- text'\ncache-mode: write", "true", []string{"cache-write-untrusted"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[ending], func(t *testing.T) {
				source := strings.ReplaceAll("on: pull_request_target\n"+tc.declaration+body, "\n", ending)
				var kinds []string
				for _, err := range testutil.WorkflowDiagnostics(t, source, "policy: {disallow-suppressions: "+tc.config+"}") {
					kinds = append(kinds, err.Rule)
				}
				if diff := cmp.Diff(kindCounts(tc.kinds), kindCounts(kinds)); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}
