package application_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/registry"
)

func TestFileOverrideToolRequirements(t *testing.T) {
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\noverrides: [{includes: [ci.yml], tools: {shellcheck: true}}]")
	options := application.Options{WorkingDir: root, ConfigFile: config, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"ci.yml", true},
		// Planning reads no workflow, so an override may still apply to a
		// composite action other.yml calls.
		{"other.yml", true},
	} {
		got, err := application.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || got["shellcheck"] != tc.want {
			t.Fatalf("%s: got %+v, %v; want shellcheck=%v", tc.path, got, err, tc.want)
		}
	}
}
