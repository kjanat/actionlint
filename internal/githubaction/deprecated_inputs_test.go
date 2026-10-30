package githubaction

import (
	"slices"
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
					"GITHUB_WORKSPACE": workspace, "INPUT_DISABLE-TOOLS": "ruff,shellcheck",
					"INPUT_PYFLAKES": pyflakes, "INPUT_ADD-PYFLAKES-TO-PATH": pathInput,
				}
				var out strings.Builder
				if code := Main(func(key string) string { return env[key] }, &out); code != 0 {
					t.Fatalf("Action status %d: %s", code, &out)
				}
				warnings := 0
				if pyflakes == "true" {
					warnings++
				}
				if got := strings.Count(out.String(), "::warning::"); got != warnings {
					t.Fatalf("want %d warnings, got %d: %s", warnings, got, &out)
				}
				if strings.Contains(out.String(), "add-pyflakes-to-path") {
					t.Fatalf("unexpected PATH deprecation warning: %s", &out)
				}
			})
		}
	}
}

func TestDeprecatedToolInputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		warnings []string
	}{
		{"defaults", map[string]string{"INPUT_SHELLCHECK": "true", "INPUT_RUFF": "true"}, nil},
		{"disabled tool", map[string]string{"INPUT_RUFF": "false"}, []string{"The ruff input is deprecated; use disable-tools: ruff."}},
		{"retired flag", map[string]string{"INPUT_PYFLAKES": "true"}, []string{"The pyflakes input is deprecated and ignored; ruff runs unless disable-tools lists it."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deprecatedInputWarnings(func(key string) string { return tc.env[key] }); !slices.Equal(got, tc.warnings) {
				t.Fatalf("warnings = %q, want %q", got, tc.warnings)
			}
		})
	}
}
