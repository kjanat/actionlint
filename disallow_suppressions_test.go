package actionlint

import (
	"strings"
	"testing"

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
	const body = "\njobs:\n  test:\n" + cachePolicySteps
	for _, tc := range []struct {
		name, declaration, config string
		kinds                     []string
	}{
		{"unused", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "true", []string{"disallow-suppressions"}},
		{"unused violation only", "cache-mode: read # actionlint:ignore cache-write-untrusted -- reviewed", "{report: violation}", nil},
		{"missing reason", "cache-mode: write # actionlint:ignore cache-write-untrusted", "true", []string{"cache-write-untrusted", "inline-suppression"}},
		{"cannot exempt restriction", "cache-mode: write # actionlint:ignore disallow-suppressions, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "inline-suppression"}},
		{"duplicates", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-write-untrusted -- reviewed", "true", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"unselected still suppressed", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-operation]}", []string{"disallow-suppressions"}},
		{"selected restored", "cache-mode: write # actionlint:ignore cache-write-untrusted, cache-operation -- reviewed", "{rules: [cache-write-untrusted]}", []string{"cache-write-untrusted", "disallow-suppressions"}},
		{"detached", "# actionlint:ignore-next-line cache-write-untrusted -- reviewed\n\ncache-mode: write", "true", []string{"cache-write-untrusted"}},
		{"quoted", "name: '# actionlint:ignore cache-write-untrusted -- text'\ncache-mode: write", "true", []string{"cache-write-untrusted"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+map[string]string{"\n": "/LF", "\r\n": "/CRLF"}[ending], func(t *testing.T) {
				source := strings.ReplaceAll("on: pull_request_target\n"+tc.declaration+body, "\n", ending)
				var kinds []string
				for _, err := range lintCachePolicy(t, source, "policy: {disallow-suppressions: "+tc.config+"}") {
					kinds = append(kinds, err.Kind)
				}
				if diff := cmp.Diff(kindCounts(tc.kinds), kindCounts(kinds)); diff != "" {
					t.Fatal(diff)
				}
			})
		}
	}
}

func TestDisallowSuppressionsDiagnostic(t *testing.T) {
	const source = "name: café # actionlint:ignore cache-operation, cache-operation -- reviewed"
	cfg, err := ParseConfig([]byte("policy: {disallow-suppressions: true}"))
	if err != nil {
		t.Fatal(err)
	}
	got := filterInlineSuppressions([]byte(source), nil, cfg.Policy.DisallowSuppressions)
	if len(got) != 1 {
		t.Fatalf("expected one directive diagnostic, got %v", got)
	}
	want := &Error{
		Line: 1, Column: 12, Kind: "disallow-suppressions",
		Message: "inline suppression of cache-operation is disallowed by policy.disallow-suppressions",
	}
	if diff := cmp.Diff(want, got[0], cmp.AllowUnexported(Error{})); diff != "" {
		t.Fatal(diff)
	}
}
