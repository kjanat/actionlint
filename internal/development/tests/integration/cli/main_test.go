package cli_test

import (
	"os"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

func TestMain(m *testing.M) {
	if testutil.RuffDiscoveryRequested() {
		os.Exit(testutil.RuffDiscoveryHelper())
	}
	if os.Getenv("ACTIONLINT_TEST_TOOL_PROCESS") == "1" {
		os.Exit(environmentToolHelper())
	}
	testutil.Main(m)
}
