package rules_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func permissionsConfig(t *testing.T, permissions string) *repoconfig.Config {
	t.Helper()
	config, err := checking.ConfigurationRules().Parse([]byte("repository: {default-workflow-permissions: " + permissions + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return config
}
