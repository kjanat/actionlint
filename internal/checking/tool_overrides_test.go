package checking_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/checking/scripts/registry"
)

func TestFileOverrideToolRequirements(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\noverrides: [{includes: [ci.yml], tools: {shellcheck: true}}]")
	options := checking.Options{WorkingDir: root, ConfigFile: config, Tools: registry.Registrations(registry.Selections{"shellcheck": {Command: "shellcheck"}})}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"ci.yml", true},
		// Planning reads no workflow, so an override may still apply to a
		// composite action other.yml calls.
		{"other.yml", true},
	} {
		got, err := checking.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || got["shellcheck"] != tc.want {
			t.Fatalf("%s: got %+v, %v; want shellcheck=%v", tc.path, got, err, tc.want)
		}
	}
}
