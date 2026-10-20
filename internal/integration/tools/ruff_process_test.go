package tools_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

func runRuffHelperProcess() {
	if prefix := os.Getenv("ACTIONLINT_TEST_RUFF_PREFIX"); prefix != "" {
		var expected []string
		if err := json.Unmarshal([]byte(prefix), &expected); err != nil || len(os.Args) <= len(expected) || !slices.Equal(os.Args[1:len(expected)+1], expected) {
			os.Exit(3)
		}
		file, err := os.OpenFile(os.Getenv("ACTIONLINT_TEST_RUFF_CALLS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(3)
		}
		if _, err := fmt.Fprintln(file, strings.Join(os.Args[1:], " ")); err != nil {
			os.Exit(3)
		}
		if err := file.Close(); err != nil {
			os.Exit(3)
		}
		os.Args = append(os.Args[:1], os.Args[len(expected)+1:]...)
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			fmt.Println("ruff 0.17.0")
			os.Exit(0)
		}
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	if os.Getenv("ACTIONLINT_TEST_RUFF_WAIT") == "1" {
		time.Sleep(10 * time.Second)
	}
	if len(os.Args) < 2 || os.Args[1] != "check" || !slices.Contains(os.Args, "--isolated") || !slices.Contains(os.Args, "--no-fix") {
		os.Exit(3)
	}
	if _, present := os.LookupEnv("RUFF_OUTPUT_FILE"); present {
		os.Exit(3)
	}
	if os.Getenv("ACTIONLINT_TEST_RUFF_FLAGS") == "1" && !slices.Equal(os.Args[2:5], []string{"--preview", "--exclude", "vendor/**"}) {
		os.Exit(3)
	}
	fmt.Print(os.Getenv("ACTIONLINT_TEST_RUFF_OUTPUT"))
	code, _ := strconv.Atoi(os.Getenv("ACTIONLINT_TEST_RUFF_EXIT"))
	os.Exit(code)
}
