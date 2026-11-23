package cli

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var pluginInstallSpellings = [][]string{
	{"plugin", "install"},
	{"plugin", "add"},
	{"plugins", "install"},
	{"plugins", "add"},
}

func TestPluginInstallSpellingsSelectInstallWithFlags(t *testing.T) {
	t.Parallel()
	for _, spelling := range pluginInstallSpellings {
		t.Run(strings.Join(spelling, " "), func(t *testing.T) {
			t.Parallel()
			app := newCommandApp(t.Context(), &Command{})
			install, _, err := app.root.Find([]string{"plugin", "install"})
			if err != nil {
				t.Fatal(err)
			}
			args := append(slices.Clone(spelling), "--registry", "file:///catalogue.json", "--version", "v1.2.3", "owner/repo")
			found, rest, err := app.root.Find(args)
			if err != nil {
				t.Fatal(err)
			}
			if found != install {
				t.Fatalf("selected %q, want %q", found.CommandPath(), install.CommandPath())
			}
			if err := found.ParseFlags(rest); err != nil {
				t.Fatal(err)
			}
			if got := app.inv.Plugin.Registry; got != "file:///catalogue.json" {
				t.Errorf("registry = %q", got)
			}
			if got := app.inv.Plugin.Version; got != "v1.2.3" {
				t.Errorf("version = %q", got)
			}
			if got := found.Flags().Args(); !slices.Equal(got, []string{"owner/repo"}) {
				t.Errorf("positional arguments = %q", got)
			}
		})
	}
}

func runPluginAliasCommand(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := Command{Stdout: &stdout, Stderr: &stderr}
	if status := cmd.MainContext(t.Context(), append([]string{"actionlint"}, args...)); status != 0 {
		t.Fatalf("%q: status=%d stdout=%s stderr=%s", args, status, &stdout, &stderr)
	}
	return stdout.String(), stderr.String()
}

func TestPluginInstallSpellingsShareHelp(t *testing.T) {
	t.Parallel()
	want, _ := runPluginAliasCommand(t, "plugin", "install", "--help", "--json")
	if !strings.Contains(want, `"usage":"actionlint plugin install [plugin] [flags]"`) {
		t.Fatalf("canonical JSON help: %s", want)
	}
	for _, spelling := range pluginInstallSpellings {
		got, _ := runPluginAliasCommand(t, append(slices.Clone(spelling), "--help", "--json")...)
		if got != want {
			t.Errorf("%q JSON help differs:\n got %s\nwant %s", spelling, got, want)
		}
		_, text := runPluginAliasCommand(t, append(slices.Clone(spelling), "--help")...)
		if !strings.Contains(text, "actionlint plugin install [plugin] [flags]") || !strings.Contains(text, "--registry") {
			t.Errorf("%q help: %s", spelling, text)
		}
	}
}

func TestPluginAliasesStayOutOfHelpAndCompletion(t *testing.T) {
	t.Parallel()
	ansi := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	listed := func(output string) []string {
		var names []string
		for line := range strings.Lines(ansi.ReplaceAllString(output, "")) {
			if fields := strings.Fields(line); len(fields) > 0 {
				names = append(names, fields[0])
			}
		}
		return names
	}
	for _, tc := range []struct {
		args          []string
		want, missing string
	}{
		{[]string{"--help"}, "plugin", "plugins"},
		{[]string{"plugin", "--help"}, "install", "add"},
		{[]string{"plugins", "--help"}, "install", "add"},
		{[]string{"--help", "--color"}, "plugin", "plugins"},
		{[]string{"--help", "--no-color"}, "plugin", "plugins"},
		{[]string{"__complete", "plug"}, "plugin", "plugins"},
		{[]string{"__complete", "plugin", ""}, "install", "add"},
		{[]string{"__complete", "plugins", ""}, "install", "add"},
	} {
		stdout, stderr := runPluginAliasCommand(t, tc.args...)
		names := listed(stdout + stderr)
		if !slices.Contains(names, tc.want) {
			t.Errorf("%q does not list %q: %s%s", tc.args, tc.want, stdout, stderr)
		}
		if slices.Contains(names, tc.missing) {
			t.Errorf("%q lists hidden alias %q: %s%s", tc.args, tc.missing, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"--help", "--json"}, {"plugin", "--help", "--json"}} {
		stdout, _ := runPluginAliasCommand(t, args...)
		for _, alias := range []string{`"name":"plugins"`, `"name":"add"`, `"aliases"`} {
			if strings.Contains(stdout, alias) {
				t.Errorf("%q JSON help contains %s: %s", args, alias, stdout)
			}
		}
	}
}
