package ruff

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		config                      Config
		selectRules, ignore, target string
	}{
		{"defaults", Config{}, "F", "", "py314"},
		{"selection", Config{Select: []string{"F", "B"}, Ignore: []string{"F401"}, TargetVersion: "py312"}, "F,B", "F401", "py312"},
		{"empty selection", Config{Select: []string{}}, "ALL", "ALL", "py314"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := arguments(tc.config)
			for flag, want := range map[string]string{"--select": tc.selectRules, "--ignore": tc.ignore, "--target-version": tc.target} {
				i := slices.Index(args, flag)
				if want == "" {
					if i != -1 {
						t.Fatalf("unexpected %s", flag)
					}
					continue
				}
				if i < 0 || i+1 >= len(args) || args[i+1] != want {
					t.Fatalf("%s: %v", flag, args)
				}
			}
			for _, flag := range []string{"--isolated", "--ignore-noqa", "--no-fix", "--no-cache"} {
				if !slices.Contains(args, flag) {
					t.Fatalf("missing %s", flag)
				}
			}
			if args[len(args)-1] != "-" {
				t.Fatal("not reading stdin")
			}
		})
	}
}

func TestShellSelectionAndFork(t *testing.T) {
	var calls int
	c := New(func(_ []string, _ string, callback func([]byte, error) error) {
		calls++
		if err := callback([]byte("[]"), nil); err != nil {
			t.Fatal(err)
		}
	}, func() error { return nil }, nil)
	check := func(c *Checker, shell *string, config Config, want int) {
		t.Helper()
		c.Check("print(1)", shell, "test", config, func(Diagnostic) { t.Fatal("unexpected diagnostic") })
		if calls != want {
			t.Fatalf("got %d calls, want %d", calls, want)
		}
	}
	python, bash, dynamic := "python3 -u {0}", "bash", "${{ inputs.shell }}"
	check(c, nil, Config{}, 0)
	c.WorkflowShell(&python)
	check(c, nil, Config{}, 1)
	c.JobShell(&bash)
	check(c, nil, Config{}, 1)
	check(c, &python, Config{}, 2)
	check(c, &dynamic, Config{}, 2)
	c.JobShell(nil)
	check(c, nil, Config{}, 3)
	check(c, nil, Config{Enabled: new(false)}, 3)
	check(c.Fork(), nil, Config{}, 3)
	check(c.Fork(), &python, Config{}, 4)
}

func TestCheckFailureAndAtomicDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		output     string
		processErr error
	}{
		{"[]", errors.New("exit 2")},
		{"null", nil},
		{`[{"code":"F821","message":"missing","location":{"row":1,"column":1}},{}]`, nil},
	} {
		var result error
		c := New(func(_ []string, _ string, callback func([]byte, error) error) {
			result = callback([]byte(tc.output), tc.processErr)
		}, func() error { return result }, nil)
		python := "python"
		c.Check("print(1)", &python, "workflow:12", Config{}, func(Diagnostic) { t.Fatal("partially emitted invalid response") })
		if err := c.Wait(); err == nil || !strings.Contains(err.Error(), "workflow:12") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestSanitize(t *testing.T) {
	source := "print(${{\n 'é'\n}}, missing)"
	got, valid := Sanitize(source, func(string) (int, bool) { return len("\n 'é'\n}}"), true })
	if !valid || len([]rune(got)) != len([]rune(source)) || strings.Contains(got, "${{") {
		t.Fatalf("%q %v", got, valid)
	}
	for i, r := range []rune(source) {
		if r == '\n' && []rune(got)[i] != r {
			t.Fatal("line break moved")
		}
	}
	for _, end := range []int{-1, 0, 1, len(source) + 1} {
		if _, valid := Sanitize(source, func(string) (int, bool) { return end, true }); valid {
			t.Fatalf("accepted end %d", end)
		}
	}
}
