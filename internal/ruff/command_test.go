package ruff

import (
	"slices"
	"strings"
	"testing"
)

func TestLauncherCommandArguments(t *testing.T) {
	for _, tc := range []struct {
		exe    string
		prefix []string
	}{
		{"env", []string{"ruff"}},
		{"/usr/bin/env", []string{"NAME=--silent", "ruff"}},
		{"env", []string{"NAME=/path/ruff", "ruff"}},
		{"env", []string{"-i", "--unset", "ruff", "--", "/tools/ruff"}},
		{"env", []string{"--unset=ruff", "ruff"}},
		{"env", []string{"--unset=/tools/ruff", "ruff"}},
		{"uvx", []string{"ruff"}},
		{"uvx", []string{"ruff@0.17.0"}},
		{"uvx", []string{"--", "ruff@latest"}},
		{`C:\tools\UVX.EXE`, []string{"--from", "ruff", "ruff"}},
		{"uvx", []string{"--from=ruff==0.17.0", "--offline", "--no-cache", "ruff"}},
		{"uvx", []string{"--from=/tools/ruff", "ruff"}},
	} {
		t.Run(tc.exe+strings.Join(tc.prefix, " "), func(t *testing.T) {
			flags := []string{"--preview", "--exclude", "ruff"}
			prefix, got, err := commandArguments(tc.exe, slices.Concat(tc.prefix, flags))
			if err != nil || !slices.Equal(prefix, tc.prefix) || !slices.Equal(got, flags) {
				t.Fatalf("prefix=%v flags=%v error=%v", prefix, got, err)
			}
		})
	}
	for _, args := range [][]string{nil, {"--from", "ruff"}, {"--index-url", "ruff", "ruff"}, {"--", "other"}, {"other", "ruff"}, {"ruff@"}, {"ruff@>=0.17"}} {
		if _, _, err := commandArguments("uvx", args); err == nil {
			t.Fatalf("ambiguous uvx arguments accepted: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"-u", "ruff"}, {"--unknown", "ruff"}, {"--chdir=/tools/ruff", "ruff"}, {"RUFF_OUTPUT_FILE=output.json", "ruff"}, {"other", "ruff"}} {
		if _, _, err := commandArguments("env", args); err == nil {
			t.Fatalf("ambiguous env arguments accepted: %v", args)
		}
	}
	flags := []string{"--preview", "--exclude", "vendor/**"}
	for _, exe := range []string{"ruff", "custom-checker"} {
		prefix, got, err := commandArguments(exe, flags)
		if err != nil || len(prefix) != 0 || !slices.Equal(got, flags) {
			t.Fatalf("direct checker arguments changed: %v, %v, %v", prefix, got, err)
		}
	}
}

func TestInvalidLauncherFailsBeforeScheduling(t *testing.T) {
	checker := New(func([]string, string, func([]byte, error) error) { t.Fatal("invalid launcher executed") }, func() error { return nil }, nil)
	checker.CommandArguments("uvx", []string{"--from", "ruff"})
	python := "python"
	for _, candidate := range []*Checker{checker, checker.Fork()} {
		if err := candidate.Check("print(1)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil {
			t.Fatal("invalid launcher appeared clean")
		}
	}
}
