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

// ShellCheck reads quotes in a directive value literally, so a path is
// wrapped in a quote type it does not contain, and left bare only when it
// holds both types and no whitespace that would start another directive key.
func TestDirectivePathQuotesWithAQuoteThePathDoesNotContain(t *testing.T) {
	for path, want := range map[string]string{
		"lib":      `"lib"`,
		"lib dir":  `"lib dir"`,
		"it's":     `"it's"`,
		`say "hi"`: `'say "hi"'`,
		`a"b'c`:    `a"b'c`,
	} {
		if got, err := directivePath(path); got != want || err != nil {
			t.Errorf("directivePath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestDirectivePathRejectsPathsShellCheckCannotRead(t *testing.T) {
	for _, path := range []string{
		"",
		"a\nb",
		"a\rb",
		"a\x00b",
		`a"b' c`,
		"a\"b'\tc",
		`"a'b`,
		`'a"b`,
	} {
		if got, err := directivePath(path); err == nil {
			t.Errorf("directivePath(%q) = %q, want an error", path, got)
		}
	}
}
