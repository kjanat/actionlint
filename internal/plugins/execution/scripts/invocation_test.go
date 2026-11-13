package scripts

import (
	"testing"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	tools "actionlint.kjanat.dev/v2/internal/checking/scripts/contract"
	"actionlint.kjanat.dev/v2/internal/languages/interpreters"
	"actionlint.kjanat.dev/v2/internal/model/commands"
)

func TestScriptRoutingUsesSharedInvocationLanguage(t *testing.T) {
	t.Parallel()
	registry, err := interpreters.New([]commands.Definition{{ID: "example/python", Language: "python", Options: []commands.Option{{Short: "u"}}}, {ID: "example/wrapper", Executables: []string{"actions-shell"}, Prefix: []string{"python"}, Delegate: "example/python"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ command, want string }{
		{"actions-shell python -u {0}", "python"},
		{"actions-shell rust {0}", "actions-shell"},
		{`"/path with spaces/bash" {0}`, `path`},
	} {
		got := scriptShell(tools.Script{Execution: tools.Execution{Shell: tc.command}, Fact: checkinputs.Script{Interpreters: registry}})
		if got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.command, got, tc.want)
		}
	}
}
