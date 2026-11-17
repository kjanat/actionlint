package backends

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/scripts"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

func TestScriptRoutingUsesSharedInvocationLanguage(t *testing.T) {
	t.Parallel()
	registry, err := commands.NewWithShells([]commands.Definition{{ID: "example/python", Language: "python", Options: []commands.Option{{Short: "u"}}}, {ID: "example/wrapper", Executables: []string{"actions-shell"}, Prefix: []string{"python"}, Delegate: "example/python"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ command, want string }{
		{"actions-shell python -u {0}", "python"},
		{"actions-shell rust {0}", "actions-shell"},
		{`"/path with spaces/bash" {0}`, `path`},
	} {
		got := scriptShell(scriptanalysis.Unit{Execution: &scripts.Execution{Shell: scripts.Shell{Name: tc.command}}, Interpreters: registry})
		if got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.command, got, tc.want)
		}
	}
}
