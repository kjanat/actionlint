package shell

import (
	"strings"
	"testing"
)

func TestParseDirectivesExpandsEnableAll(t *testing.T) {
	for _, enable := range []string{`"all"`, `true`, `["all"]`} {
		directives, err := ParseDirectives([]byte(`{"enable": ` + enable + `, "disable": ["SC2086"], "extended-analysis": false, "source-path": ["lib dir"]}`))
		if err != nil {
			t.Fatalf("enable %s: %v", enable, err)
		}
		rendered, err := RenderDirectives(directives)
		if err != nil {
			t.Fatal(err)
		}
		want := "# shellcheck disable=SC2086\n# shellcheck enable=all\n# shellcheck extended-analysis=false\n# shellcheck source-path=\"lib dir\"\n"
		if rendered != want {
			t.Fatalf("enable %s rendered %q, want %q", enable, rendered, want)
		}
	}
}

func TestParseDirectivesNamesTheOffendingKey(t *testing.T) {
	for document, want := range map[string]string{
		`{"disable": ["SC2086x"]}`:          "disable: invalid code selector",
		`{"enable": ["bad name"]}`:          "enable: invalid check name",
		`{"shell": "fish"}`:                 "shell: unknown dialect",
		`{"source-path": ["a'b\" c"]}`:      "source-path: ShellCheck cannot represent path",
		`{"unknown": true}`:                 "unknown field",
		`{"enable": {"not": "a list"}}`:     "enable:",
		`{"external-sources": "sometimes"}`: "external-sources",
	} {
		if _, err := ParseDirectives([]byte(document)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want an error containing %q", document, err, want)
		}
	}
}

func TestRenderDirectivesOfNoneIsEmpty(t *testing.T) {
	if rendered, err := RenderDirectives(nil); rendered != "" || err != nil {
		t.Fatalf("got %q, %v", rendered, err)
	}
}
