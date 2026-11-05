package tools_test

import (
	"os"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

func TestMain(m *testing.M) {
	if os.Getenv("ACTIONLINT_TEST_RUFF") == "1" {
		runRuffHelperProcess()
		return
	}
	if os.Getenv("ACTIONLINT_TEST_STDIN_HELPER") == "1" {
		os.Exit(m.Run())
	}
	testutil.Main(m)
}
