package cli_test

import (
	"os"
	"strings"
	"testing"

	fixtureregistry "actionlint.kjanat.dev/v2/internal/development/tests/support/registry"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
)

func TestConfigInitSnapshotSchemaDirective(t *testing.T) {
	for _, version := range []string{"2.0.0-SNAPSHOT-02d216d", "2.0.0-rc.1-SNAPSHOT-02d216d"} {
		t.Run(version, func(t *testing.T) {
			fixtureregistry.NewDefaultPluginRegistry(t, "shellcheck", "ruff")
			t.Chdir(commandTestRepo(t))
			previous := buildinfo.ReleaseVersion
			buildinfo.ReleaseVersion = version
			t.Cleanup(func() { buildinfo.ReleaseVersion = previous })
			got := testRunCommand("", "config", "init")
			if got.Status != 0 {
				t.Fatal(got)
			}
			content, err := os.ReadFile(".github/actionlint.yaml")
			if err != nil {
				t.Fatal(err)
			}
			header, _, _ := strings.Cut(string(content), "\n")
			const want = "# yaml-language-server: $schema=https://cdn.jsdelivr.net/gh/kjanat/actionlint@02d216d/actionlint.schema.json"
			if header != want {
				t.Fatalf("config init schema directive = %q, want %q", header, want)
			}
		})
	}
}
