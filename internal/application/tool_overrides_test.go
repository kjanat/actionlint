package application_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/tools/adapters"
)

func TestFileOverrideToolRequirements(t *testing.T) {
	root := t.TempDir()
	config := writeFixture(t, root, "actionlint.yml", "tools: {shellcheck: false}\noverrides: [{includes: [ci.yml], tools: {shellcheck: true}}]")
	options := application.Options{WorkingDir: root, ConfigFile: config, Tools: (adapters.Options{ShellCheck: "shellcheck"}).Registrations()}
	for _, tc := range []struct {
		path string
		want bool
	}{{"ci.yml", true}, {"other.yml", false}} {
		got, err := application.RequiredTools(t.Context(), options, []string{tc.path})
		if err != nil || got["shellcheck"] != tc.want {
			t.Fatalf("%s: got %+v, %v; want shellcheck=%v", tc.path, got, err, tc.want)
		}
	}
}
