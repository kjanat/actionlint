package githubaction

import (
	"strings"
	"testing"
)

func TestDeprecatedPyflakesInputsWarnOnlyWhenRequested(t *testing.T) {
	workspace := workspaceWith(t, map[string]string{
		".git": "", ".github/workflows/test.yml": cleanWorkflow,
	})
	for _, pathInput := range []string{"", "false", "true"} {
		for _, pyflakes := range []string{"", "false", "true"} {
			t.Run("path="+pathInput+"/pyflakes="+pyflakes, func(t *testing.T) {
				env := map[string]string{
					"GITHUB_WORKSPACE": workspace, "INPUT_SHELLCHECK": "false",
					"INPUT_PYFLAKES": pyflakes, "INPUT_ADD-PYFLAKES-TO-PATH": pathInput,
				}
				var out strings.Builder
				if code := Main(func(key string) string { return env[key] }, &out); code != 0 {
					t.Fatalf("Action status %d: %s", code, &out)
				}
				warnings := 0
				if pathInput == "true" {
					warnings++
				}
				if pyflakes == "true" {
					warnings++
				}
				if got := strings.Count(out.String(), "::warning::"); got != warnings {
					t.Fatalf("want %d warnings, got %d: %s", warnings, got, &out)
				}
				if got := strings.Contains(out.String(), "add-pyflakes-to-path input is deprecated and ignored"); got != (pathInput == "true") {
					t.Fatalf("unexpected PATH deprecation warning: %s", &out)
				}
			})
		}
	}
}
