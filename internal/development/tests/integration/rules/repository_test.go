package rules_test

import (
	"testing"

	checking "actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
)

func permissionsConfig(t *testing.T, permissions string) *configuration.Config {
	t.Helper()
	config, err := checking.ConfigurationRules().ParseConfig([]byte("repository: {default-workflow-permissions: " + permissions + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return config
}
