package builtin_test

import (
	"testing"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

func permissionsConfig(t *testing.T, permissions string) *repoconfig.Config {
	t.Helper()
	config, err := repoconfig.Parse([]byte("repository: {default-workflow-permissions: " + permissions + "}"))
	if err != nil {
		t.Fatal(err)
	}
	return config
}
