package cli_test

import (
	"os"
	"testing"

	"actionlint.kjanat.dev/v2/internal/testutil"
)

func TestMain(m *testing.M) {
	if os.Getenv("ACTIONLINT_TEST_RUFF_DISCOVERY") == "1" {
		os.Exit(testutil.RuffDiscoveryHelper())
	}
	if os.Getenv("ACTIONLINT_TEST_TOOL_PROCESS") == "1" {
		os.Exit(environmentToolHelper())
	}
	testutil.Main(m)
}
