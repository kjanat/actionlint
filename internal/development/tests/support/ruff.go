package testutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RuffDiscoveryRequested reports whether this process should act as the
// fake Ruff of RuffDiscoveryHelper: ACTIONLINT_TEST_RUFF_DISCOVERY is 1, or
// the executable is named ruff. Tool providers run with a scrubbed
// environment, so a copy of a test binary found on PATH as ruff sees no
// test variables.
func RuffDiscoveryRequested() bool {
	if os.Getenv("ACTIONLINT_TEST_RUFF_DISCOVERY") == "1" {
		return true
	}
	executable, err := os.Executable()
	return err == nil && strings.TrimSuffix(strings.ToLower(filepath.Base(executable)), ".exe") == "ruff"
}

// RuffDiscoveryHelper acts as Ruff for discovery tests. It appends its
// command line to the file ACTIONLINT_TEST_RUFF_CALLS names, answers
// --version with ACTIONLINT_TEST_RUFF_VERSION and checks scripts only as
// Ruff 0.17.0. Without ACTIONLINT_TEST_RUFF_CALLS it uses the files "calls"
// and "version" beside its executable instead; see RuffDiscoveryFixture.
func RuffDiscoveryHelper() int {
	calls, version := os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.Getenv("ACTIONLINT_TEST_RUFF_VERSION")
	if calls == "" {
		executable, err := os.Executable()
		if err != nil {
			return 3
		}
		directory := filepath.Dir(executable)
		file, err := os.OpenFile(filepath.Join(directory, "version"), os.O_RDONLY, 0)
		if err != nil {
			return 3
		}
		data, readErr := io.ReadAll(file)
		if err := file.Close(); err != nil || readErr != nil {
			return 3
		}
		calls, version = filepath.Join(directory, "calls"), strings.TrimSpace(string(data))
	}
	file, err := os.OpenFile(calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 3
	}
	_, writeErr := fmt.Fprintln(file, strings.Join(os.Args[1:], " "))
	if err := file.Close(); err != nil || writeErr != nil {
		return 3
	}
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

// RuffDiscoveryFixture sets the version the fake Ruff in directory reports
// and clears its recorded calls. It returns the path of the calls file.
func RuffDiscoveryFixture(t *testing.T, directory, version string) string {
	t.Helper()
	calls := filepath.Join(directory, "calls")
	if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "version"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return calls
}
