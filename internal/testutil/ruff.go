package testutil

import (
	"fmt"
	"os"
	"strings"
)

func RuffDiscoveryHelper() int {
	file, err := os.OpenFile(os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 3
	}
	_, writeErr := fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
	if err := file.Close(); err != nil || writeErr != nil {
		return 3
	}
	version := os.Getenv("ACTIONLINT_TEST_RUFF_VERSION")
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return 0
	}
	if version != "ruff 0.17.0" {
		fmt.Fprintln(os.Stderr, "unsupported target-version: requires newer Ruff")
		return 2
	}
	fmt.Println("[]")
	return 0
}
