package cli

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("ACTIONLINT_TEST_TOOL_PROCESS") == "1" {
		os.Exit(environmentToolHelper())
	}
	if err := os.Chdir("../.."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
