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
		{"python", []string{"-m", "ruff"}},
		{"python3", []string{"-mruff"}},
		{"/usr/bin/python3.12", []string{"-IuB", "-W", "error", "-X", "utf8", "-m", "ruff"}},
		{`C:\Program Files\Python\PYTHON3.13.EXE`, []string{"-bb", "-OO", "-Wignore", "-Xdev", "-m", "ruff"}},
		{"python3.14", []string{"--check-hash-based-pycs", "always", "-Em", "ruff"}},
		{"python", []string{"-umruff"}},
		{"python", []string{"-W", "-m", "-X", "-m", "-m", "ruff"}},
		{"py", []string{"-3.12-64", "-u", "-m", "ruff"}},
		{`C:\Windows\PY.EXE`, []string{"-V:PythonCore/3.13", "-Xutf8", "-m", "ruff"}},
		{"py", []string{"-V:3", "-m", "ruff"}},
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
	for _, exe := range []string{"python", "/usr/bin/python3.12", `C:\Python\python.exe`, "py"} {
		for _, args := range [][]string{
			nil, {"-m"}, {"-m", "other", "ruff"}, {"-mother", "ruff"}, {"-c", "import ruff"}, {"ruff.py"},
			{"-W", "-m", "ruff"}, {"-X", "-m", "ruff"}, {"-W"}, {"-X", ""},
			{"--", "-m", "ruff"}, {"--version", "-m", "ruff"}, {"-V", "-m", "ruff"}, {"-h", "-m", "ruff"},
			{"--help", "-m", "ruff"}, {"-i", "-m", "ruff"}, {"-z", "-m", "ruff"},
			{"--check-hash-based-pycs"}, {"--check-hash-based-pycs", "sometimes", "-m", "ruff"},
			{"-m", "RUFF"}, {"-m", "ruff.__main__"}, {"-m", "ruff@0.17.0"}, {"-2", "-m", "ruff"},
			{"-V:Other/3.12", "-m", "ruff"}, {"-V:3.", "-m", "ruff"}, {"-3.12-128", "-m", "ruff"},
			{"-3.12", "-3.13", "-m", "ruff"},
		} {
			if _, _, err := commandArguments(exe, args); err == nil || !strings.Contains(err.Error(), "-m ruff") {
				t.Fatalf("unsupported Python launcher accepted or error not actionable: exe=%q args=%v error=%v", exe, args, err)
			}
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
	for _, launcher := range []struct {
		exe  string
		args []string
	}{
		{"uvx", []string{"--from", "ruff"}},
		{"python", []string{"-c", "import ruff"}},
		{"python3.12", []string{"-m", "other"}},
		{"py.exe", []string{"ruff.py"}},
	} {
		checker := New(func([]string, string, func([]byte, error) error) { t.Fatal("invalid launcher executed") }, func() error { return nil }, nil)
		checker.CommandArguments(launcher.exe, launcher.args)
		python := "python"
		for _, candidate := range []*Checker{checker, checker.Fork()} {
			if err := candidate.Check("print(1)", &python, "workflow:12", Config{}, func(Diagnostic) {}); err == nil {
				t.Fatal("invalid launcher appeared clean")
			}
		}
	}
}
