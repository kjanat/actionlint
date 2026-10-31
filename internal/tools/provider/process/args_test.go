package process

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

func bundledManifest(t *testing.T, name string) provider.Manifest {
	t.Helper()
	manifests, err := provider.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if manifest.Rule.Name == name {
			return manifest
		}
	}
	t.Fatalf("no bundled provider %s", name)
	return provider.Manifest{}
}

func TestRenderArgs(t *testing.T) {
	templates := []string{"literal", "{var:path}", "{var:unset}", "{flag:dialect:--shell}", "{flag:unset:--opt}", "{option:dialect:--shell}", "{option:unset:--opt}", "{switch:on:-x:}", "{switch:off:-x:--no-x}", "{switch:false:--yes:--no}", "{list:exclude:,}", "{list:unset:,}", "{config:toml-pairs:--config}", "{user-args}", "{config:toml-file}", "-"}
	got, err := renderArgs(templates, argInputs{
		vars:       map[string]string{"path": "a b", "dialect": "bash", "on": "1", "false": "false", "exclude": "SC1\nSC2"},
		user:       []string{"--user", "value"},
		tomlPairs:  []string{"--config", `"k" = 1`},
		configFile: "/tmp/config.toml",
	})
	want := []string{"literal", "a b", "--shell", "bash", "--shell=bash", "-x", "--no-x", "--no", "SC1,SC2", "--config", `"k" = 1`, "--user", "value", "/tmp/config.toml", "-"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	for _, template := range []string{"{unknown:x}", "{switch:x}", "{config:yaml}"} {
		if _, err := renderArgs([]string{template}, argInputs{}); err == nil {
			t.Fatalf("accepted %s", template)
		}
	}
}

func ruffLaunchers(t *testing.T) []launcher {
	t.Helper()
	launchers, err := compileLaunchers(bundledManifest(t, "ruff").Process.UserArgs.Launchers)
	if err != nil {
		t.Fatal(err)
	}
	return launchers
}

// The Ruff launcher grammar is data. These cases are those of the Ruff
// adapter's commandArguments test, run against the bundled manifest.
func TestRuffLauncherGrammar(t *testing.T) {
	launchers := ruffLaunchers(t)
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
			prefix, got, err := splitLauncher(launchers, tc.exe, slices.Concat(tc.prefix, flags))
			if err != nil || !slices.Equal(prefix, tc.prefix) || !slices.Equal(got, flags) {
				t.Fatalf("prefix=%v flags=%v error=%v", prefix, got, err)
			}
		})
	}
	for _, args := range [][]string{nil, {"--from", "ruff"}, {"--index-url", "ruff", "ruff"}, {"--", "other"}, {"other", "ruff"}, {"ruff@"}, {"ruff@>=0.17"}} {
		if _, _, err := splitLauncher(launchers, "uvx", args); err == nil {
			t.Fatalf("ambiguous uvx arguments accepted: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"-u", "ruff"}, {"--unknown", "ruff"}, {"--chdir=/tools/ruff", "ruff"}, {"RUFF_OUTPUT_FILE=output.json", "ruff"}, {"ruff_output_file=output.json", "ruff"}, {"other", "ruff"}} {
		if _, _, err := splitLauncher(launchers, "env", args); err == nil {
			t.Fatalf("ambiguous env arguments accepted: %v", args)
		}
	}
	for _, exe := range []string{"python", "/usr/bin/python3.12", `C:\Python\python.exe`, "py"} {
		for _, args := range [][]string{
			nil,
			{"-m"},
			{"-m", "other", "ruff"},
			{"-mother", "ruff"},
			{"-c", "import ruff"},
			{"ruff.py"},
			{"-W", "-m", "ruff"},
			{"-X", "-m", "ruff"},
			{"-W"},
			{"-X", ""},
			{"--", "-m", "ruff"},
			{"--version", "-m", "ruff"},
			{"-V", "-m", "ruff"},
			{"-h", "-m", "ruff"},
			{"--help", "-m", "ruff"},
			{"-i", "-m", "ruff"},
			{"-z", "-m", "ruff"},
			{"--check-hash-based-pycs"},
			{"--check-hash-based-pycs", "sometimes", "-m", "ruff"},
			{"-m", "RUFF"},
			{"-m", "ruff.__main__"},
			{"-m", "ruff@0.17.0"},
			{"-2", "-m", "ruff"},
			{"-V:Other/3.12", "-m", "ruff"},
			{"-V:3.", "-m", "ruff"},
			{"-3.12-128", "-m", "ruff"},
			{"-3.12", "-3.13", "-m", "ruff"},
		} {
			if _, _, err := splitLauncher(launchers, exe, args); err == nil || !strings.Contains(err.Error(), "-m ruff") {
				t.Fatalf("unsupported Python launcher accepted or error not actionable: exe=%q args=%v error=%v", exe, args, err)
			}
		}
	}
	flags := []string{"--preview", "--exclude", "vendor/**"}
	for _, exe := range []string{"ruff", "custom-checker"} {
		prefix, got, err := splitLauncher(launchers, exe, flags)
		if err != nil || len(prefix) != 0 || !slices.Equal(got, flags) {
			t.Fatalf("direct checker arguments changed: %v, %v, %v", prefix, got, err)
		}
	}
}

// The Ruff user-argument rules are data. These cases are those the Ruff
// adapter's validateCommand rejects or preserves.
func TestRuffUserArgs(t *testing.T) {
	spec := bundledManifest(t, "ruff").Process.UserArgs
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ruff.toml"), []byte("line-length = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--select", "F", "--ignore=F401", "--exclude", "vendor/**"},
		{"--config", `lint.dummy-variable-rgx = "--silent"`, "--color", "never"},
		{"--exclude", "generated", "--extend-exclude", "other.py"},
		{"--per-file-ignores", "*.py:F401", "--extend-select", "B", "--preview"},
		{"--config=line-length = 100"},
		{"--config", `lint.per-file-ignores = {"a=b" = ["F"]}`},
	} {
		if err := checkUserArgs("ruff", spec, args, directory); err != nil {
			t.Errorf("args %q rejected: %v", args, err)
		}
	}
	for _, tc := range []struct {
		args   []string
		reason string
	}{
		{[]string{"other.py"}, "file operands"},
		{[]string{"."}, "file operands"},
		{[]string{""}, "file operands"},
		{[]string{"-"}, "file operands"},
		{[]string{"--", "other.py"}, "file operands"},
		{[]string{"--select", "F", "other.py"}, "file operands"},
		{[]string{"--exclude=vendor", "other.py"}, "file operands"},
		{[]string{"--exclude", "@args"}, "argument files"},
		{[]string{"--show-settings"}, "inspection output"},
		{[]string{"--diff"}, "editing output"},
		{[]string{"--statistics"}, "statistics output"},
		{[]string{"--output-file=out.json"}, "output redirection"},
		{[]string{"-eo", "out.json"}, "output redirection"},
		{[]string{"-qs"}, "silent output"},
		{[]string{"--extension", "py:python"}, "source-type overrides"},
		{[]string{"-h"}, "non-diagnostic modes"},
		{[]string{"--watch"}, "non-diagnostic modes"},
		{[]string{"-n"}, "--no-cache"},
		{[]string{"--target-version=py312"}, "integration-owned"},
		{[]string{"--stdin-filename", "x.py"}, "integration-owned"},
		{[]string{"--config"}, "requires a value"},
		{[]string{"--config="}, "requires a value"},
		{[]string{"--config", " "}, "requires a value"},
		{[]string{"--config", "ruff.toml"}, "not a configuration file"},
		{[]string{"--config", "line-length # = 1"}, "not a configuration file"},
		{[]string{"--select"}, "requires a value"},
		{[]string{"--select", "--preview"}, "requires a value"},
	} {
		if err := checkUserArgs("ruff", spec, tc.args, directory); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("args %q: %v, want %q", tc.args, err, tc.reason)
		}
	}
}

func TestShellCheckOverrides(t *testing.T) {
	spec := bundledManifest(t, "shellcheck").Process.UserArgs
	for _, tc := range []struct {
		args    []string
		dropped bool
	}{
		{[]string{"--shell=bash"}, true},
		{[]string{"--shell", "bash"}, true},
		{[]string{"-s", "sh"}, true},
		{[]string{"-xs", "sh"}, true},
		{[]string{"-ssh"}, true},
		{[]string{"--exclude", "--shell"}, false},
		{[]string{"-e", "-s"}, false},
		{[]string{"-esSC2086"}, false},
		{[]string{"-Calways"}, false},
		{[]string{"--", "--shell=bash"}, false},
		{[]string{"-x", "file.sh"}, false},
	} {
		dropped := overriddenVars(spec, tc.args)
		if tc.dropped != slices.Equal(dropped, []string{"dialect"}) || !tc.dropped && len(dropped) != 0 {
			t.Errorf("args %q dropped %q", tc.args, dropped)
		}
	}
	t.Setenv("SHELLCHECK_OPTS", "-s  ksh")
	passthrough := bundledManifest(t, "shellcheck").Process.EnvPassthrough
	args, values := environmentOptions(spec, passthrough, nil)
	if !slices.Equal(args, []string{"-s", "ksh"}) || values["SHELLCHECK_OPTS"] != "-s  ksh" {
		t.Fatalf("inherited options: %q %v", args, values)
	}
	args, _ = environmentOptions(spec, passthrough, []string{"SHELLCHECK_OPTS=--norc"})
	if !slices.Equal(args, []string{"--norc"}) {
		t.Fatalf("command environment did not override the host: %q", args)
	}
	if args, _ := environmentOptions(spec, nil, nil); len(args) != 0 {
		t.Fatalf("variable read without passthrough: %q", args)
	}
}
