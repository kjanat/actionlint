package workspace_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
)

func TestFileOverrideToolConfigOrigins(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"tools: {shellcheck: {config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: {shellcheck: false}}]",
		"overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]",
	} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			overlay, err := checking.ConfigurationRules().ParseOverlay("config", []byte(content))
			if err != nil {
				t.Fatal(err)
			}
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: t.TempDir(), ConfigOverlays: []repoconfig.Overlay{overlay}, Rules: checking.ConfigurationRules()})
			if err != nil {
				t.Fatal(err)
			}
			config, err := session.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := checking.ConfigurationRules().ForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if got.Tools["shellcheck"].Config == nil || !got.Tools["shellcheck"].Config.FromInput() {
				t.Fatal("lost working-directory origin for input-supplied tool configuration")
			}
		})
	}
}
