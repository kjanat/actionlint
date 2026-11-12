package cli_test

import (
	"os"
	"slices"
	"testing"

	testutil "actionlint.kjanat.dev/v2/internal/development/tests/support"
)

func TestMain(m *testing.M) {
	testutil.BlockExternalHTTP()
	if testutil.RuffDiscoveryRequested() {
		os.Exit(testutil.RuffDiscoveryHelper())
	}
	if os.Getenv("ACTIONLINT_TEST_TOOL_PROCESS") == "1" {
		os.Exit(environmentToolHelper())
	}
	// Tool helpers receive arguments after -- and run in the checked project's directory.
	if slices.Contains(os.Args, "--") {
		os.Exit(m.Run())
	}
	testutil.Main(m)
}
