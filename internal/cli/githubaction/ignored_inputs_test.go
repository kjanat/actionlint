package githubaction

import (
	"maps"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/integrations/dependencycache"
)

func TestToolInputsAreIgnored(t *testing.T) {
	t.Parallel()
	ignored := map[string]string{"INPUT_SHELLCHECK": "false", "INPUT_RUFF": "false", "INPUT_PYFLAKES": "not a boolean"}
	var req lintRequest
	req.configureTools(func(key string) string { return ignored[key] })
	if len(req.disabledTools) != 0 {
		t.Fatalf("ignored inputs disabled %q", req.disabledTools)
	}

	workspace := workspaceWith(t, map[string]string{".git": "", ".github/workflows/test.yml": cleanWorkflow})
	env := map[string]string{"GITHUB_WORKSPACE": workspace, "INPUT_DISABLE-TOOLS": "ruff,shellcheck"}
	maps.Copy(env, ignored)
	var out strings.Builder
	if code := Main(func(key string) string { return env[key] }, &out, dependencycache.Acquisition{}); code != 0 {
		t.Fatalf("Action status %d: %s", code, &out)
	}
	if strings.Contains(out.String(), "::warning::") {
		t.Fatalf("unexpected warning: %s", &out)
	}
}
