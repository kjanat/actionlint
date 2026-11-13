package policy

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

func TestRegexIgnoresMatchSeparateDiagnosticCodes(t *testing.T) {
	t.Parallel()
	const message = "Double quote to prevent globbing and word splitting"
	for _, tc := range []struct {
		pattern string
		ignored bool
	}{
		{"SC2086", true},
		{"^SC2086:", true},
		{"^SC2086: Double quote", true},
		{"^Double quote", true},
		{"SC2016", false},
	} {
		for _, scope := range []string{"global", "path"} {
			t.Run(scope+"/"+tc.pattern, func(t *testing.T) {
				t.Parallel()
				patterns, err := repoconfig.CompileIgnorePatterns([]string{tc.pattern})
				if err != nil {
					t.Fatal(err)
				}
				finding := &findingmodel.Finding{Rule: "shellcheck", Code: "SC2086", Message: message}
				var policy Policy
				var paths []repoconfig.PathConfig
				if scope == "global" {
					policy.IgnorePatterns = patterns
				} else {
					paths = []repoconfig.PathConfig{{Ignore: patterns}}
				}
				got := filterErrors(policy, []*findingmodel.Finding{finding}, paths)
				if (len(got) == 0) != tc.ignored || finding.Code != "SC2086" || finding.Message != message {
					t.Fatalf("ignore=%q ignored=%t findings=%+v original=%+v", tc.pattern, tc.ignored, got, finding)
				}
			})
		}
	}
}
