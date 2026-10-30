// Command ruff-probe is a fake Ruff that answers only --version. Tool
// providers run with a scrubbed environment, so it reads the version to
// print from the file "version" beside its executable and appends each
// command line to the file "calls" there.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	directory := filepath.Dir(executable)
	version, err := os.ReadFile(filepath.Join(directory, "version"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	file, err := os.OpenFile(filepath.Join(directory, "calls"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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
	fmt.Print(string(version))
}
