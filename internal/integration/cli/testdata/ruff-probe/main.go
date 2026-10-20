package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	file, err := os.OpenFile(os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	_, writeErr := fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
	if err := file.Close(); err != nil || writeErr != nil {
		os.Exit(3)
	}
	if len(os.Args) != 2 || os.Args[1] != "--version" {
		fmt.Fprintln(os.Stderr, "expected only --version")
		os.Exit(2)
	}
	fmt.Println(os.Getenv("ACTIONLINT_TEST_RUFF_VERSION"))
}
