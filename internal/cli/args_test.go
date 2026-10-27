package cli

import (
	"slices"
	"testing"
)

func TestRootFlagsBetweenFilenames(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name       string
		args, want []string
		forced     string
		terminated bool
		json       bool
	}{
		{name: "after file", args: []string{"first.yml", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "between files", args: []string{"first.yml", "--json", "second.yml"}, want: []string{"first.yml", "second.yml"}, json: true},
		{name: "after stdin", args: []string{"-", "--json"}, want: []string{"-"}, json: true},
		{name: "single dash option", args: []string{"first.yml", "-json"}, want: []string{"first.yml"}, json: true},
		{name: "option-like value", args: []string{"first.yml", "--stdin-filename", "--json", "-"}, want: []string{"first.yml", "-"}},
		{name: "delimiter value", args: []string{"first.yml", "--stdin-filename", "--", "--json"}, want: []string{"first.yml"}, json: true},
		{name: "delimiter", args: []string{"first.yml", "--", "--json", "-h"}, want: []string{"first.yml", "--json", "-h"}, terminated: true},
		{name: "delimiter after flag", args: []string{"first.yml", "--json", "--", "--help"}, want: []string{"first.yml", "--help"}, terminated: true, json: true},
		{name: "command after file", args: []string{"first.yml", "version", "--json"}, want: []string{"first.yml", "version"}, json: true},
		{name: "command boundary", args: []string{"check", "first.yml", "--json"}, want: []string{"check", "first.yml", "--json"}},
		{name: "forced command", args: []string{"first.yml", "--command", "check", "--color=never", "--json"}, want: []string{"first.yml", "--color=never", "--json"}, forced: "check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCommandApp(&Command{})
			rest, forced, terminated, err := app.parseRoot(tc.args)
			if err != nil || !slices.Equal(rest, tc.want) || forced != tc.forced || terminated != tc.terminated || app.inv.JSON != tc.json || app.errorJSON != tc.json {
				t.Fatalf("paths=%v forced=%q terminated=%v json=%v errorJSON=%v err=%v", rest, forced, terminated, app.inv.JSON, app.errorJSON, err)
			}
		})
	}
}

func TestRootUsesCommandFlagDefinitions(t *testing.T) {
	app := newCommandApp(&Command{})
	var enabled bool
	app.root.Flags().BoolVarP(&enabled, "test-option", "t", false, "")
	rest, _, _, err := app.parseRoot([]string{"-t", "input.yml"})
	if err != nil || !enabled || !app.set["test-option"] || len(rest) != 1 || rest[0] != "input.yml" {
		t.Fatalf("root did not use the shared flag definition: enabled=%v rest=%v err=%v", enabled, rest, err)
	}
}

func TestSubcommandLegacyFlagBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args, want []string
	}{
		{"option-like value", []string{"check", "-stdin-filename", "-unknown", "-"}, []string{"check", "--stdin-filename", "-unknown", "-"}},
		{"delimiter value", []string{"check", "-stdin-filename", "--", "-unknown"}, []string{"check", "--stdin-filename", "--", "--unknown"}},
		{"literal filename", []string{"check", "--", "-unknown"}, []string{"check", "--", "-unknown"}},
		{"nested command", []string{"config", "show", "-origin"}, []string{"config", "show", "--origin"}},
		{"parent flag", []string{"config", "-no-config", "show", "-origin"}, []string{"config", "--no-config", "show", "--origin"}},
		{"command-like value", []string{"config", "-config", "show", "show", "-origin"}, []string{"config", "--config", "show", "show", "--origin"}},
		{"short value", []string{"check", "-f", "-unknown", "-"}, []string{"check", "--template", "-unknown", "-"}},
		{"short flags", []string{"check", "-q", "-ojson", "-"}, []string{"check", "--quiet", "--output-format=json", "-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newCommandApp(&Command{})
			if got := normalizeCommandArgs(app.root, tc.args); !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
