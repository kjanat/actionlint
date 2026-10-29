package workspace_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/workspace"
)

func TestFileOverrideToolConfigOrigins(t *testing.T) {
	for _, content := range []string{
		"tools: {shellcheck: {config: .shellcheckrc}}\noverrides: [{includes: [ci.yml], tools: {shellcheck: false}}]",
		"overrides: [{includes: [ci.yml], tools: {shellcheck: {config: .shellcheckrc}}}]",
	} {
		t.Run(content, func(t *testing.T) {
			overlay, err := repoconfig.ParseOverlay("config", []byte(content))
			if err != nil {
				t.Fatal(err)
			}
			session, err := workspace.New(t.Context(), workspace.Options{WorkingDir: t.TempDir(), ConfigOverlays: []repoconfig.Overlay{overlay}})
			if err != nil {
				t.Fatal(err)
			}
			config, err := session.ConfigForProject(nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := repoconfig.ForFile(config, "ci.yml", "")
			if err != nil {
				t.Fatal(err)
			}
			if got.Tools.Shellcheck.Config == nil || !got.Tools.Shellcheck.Config.FromInput() {
				t.Fatal("lost working-directory origin for input-supplied tool configuration")
			}
		})
	}
}
