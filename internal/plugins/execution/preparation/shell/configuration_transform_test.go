package shell

import (
	"encoding/json"
	"strings"
	"testing"

	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"
	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests"
	"actionlint.kjanat.dev/v2/internal/plugins/manifests/bundled"
)

func shellcheckConfiguration(t *testing.T, inline []byte) (integrationconfig.Settings, error) {
	t.Helper()
	manifests, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		if manifest.Rule.Name != "shellcheck" {
			continue
		}
		transform, err := integrationconfig.New(manifest, bundled.Program)
		if err != nil {
			t.Fatal(err)
		}
		return transform.Apply(t.Context(), inline, "", nil)
	}
	t.Fatal("no ShellCheck integration")
	return integrationconfig.Settings{}, nil
}

func quotedDirectivePath(t *testing.T, path string) (string, error) {
	t.Helper()
	inline, err := json.Marshal(map[string]any{"source-path": []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := shellcheckConfiguration(t, inline)
	return strings.TrimSuffix(strings.TrimPrefix(settings.Prefix, "# shellcheck source-path="), "\n"), err
}

func TestConfigurationTransformExpandsEnableAll(t *testing.T) {
	t.Parallel()
	for _, enable := range []string{`"all"`, `true`, `["all"]`} {
		directives, err := shellcheckConfiguration(t, []byte(`{"enable": `+enable+`, "disable": ["SC2086"], "extended-analysis": false, "source-path": ["lib dir"]}`))
		if err != nil {
			t.Fatalf("enable %s: %v", enable, err)
		}
		rendered := directives.Prefix
		want := "# shellcheck disable=SC2086\n# shellcheck enable=all\n# shellcheck extended-analysis=false\n# shellcheck source-path=\"lib dir\"\n"
		if rendered != want {
			t.Fatalf("enable %s rendered %q, want %q", enable, rendered, want)
		}
	}
}

func TestParseDirectivesNamesTheOffendingKey(t *testing.T) {
	t.Parallel()
	for document, want := range map[string]string{
		`{"disable": ["SC2086x"]}`:          "disable",
		`{"enable": ["bad name"]}`:          "enable",
		`{"shell": "fish"}`:                 "shell",
		`{"source-path": ["a'b\" c"]}`:      "source-path",
		`{"unknown": true}`:                 "unknown",
		`{"enable": {"not": "a list"}}`:     "enable",
		`{"external-sources": "sometimes"}`: "external-sources",
	} {
		if _, err := shellcheckConfiguration(t, []byte(document)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want an error containing %q", document, err, want)
		}
	}
}

func TestRenderDirectivesOfNoneIsEmpty(t *testing.T) {
	t.Parallel()
	if settings, err := shellcheckConfiguration(t, nil); settings.Prefix != "" || err != nil {
		t.Fatalf("got %q, %v", settings.Prefix, err)
	}
}

// ShellCheck reads quotes in a directive value literally, so a path is
// wrapped in a quote type it does not contain, and left bare only when it
// holds both types and no whitespace that would start another directive key.
func TestDirectivePathQuotesWithAQuoteThePathDoesNotContain(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"lib":      `"lib"`,
		"lib dir":  `"lib dir"`,
		"it's":     `"it's"`,
		`say "hi"`: `'say "hi"'`,
		`a"b'c`:    `a"b'c`,
	} {
		if got, err := quotedDirectivePath(t, path); got != want || err != nil {
			t.Errorf("directivePath(%q) = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestDirectivePathRejectsPathsShellCheckCannotRead(t *testing.T) {
	t.Parallel()
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
		if got, err := quotedDirectivePath(t, path); err == nil {
			t.Errorf("directivePath(%q) = %q, want an error", path, got)
		}
	}
}

func prepareShellcheck(t *testing.T, run Run, source string) (preparation.Prepared, error) {
	t.Helper()
	providers, err := manifests.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		if provider.Rule.Name != "shellcheck" {
			continue
		}
		run.Transform, err = preparation.NewSourceTransform(provider.PreparationTransform, bundled.Program)
		if err != nil {
			t.Fatal(err)
		}
		return PrepareRun(t.Context(), run, source)
	}
	t.Fatal("missing ShellCheck integration")
	return preparation.Prepared{}, nil
}
