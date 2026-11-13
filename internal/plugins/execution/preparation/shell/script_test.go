package shell

import (
	"encoding/json"
	"testing"

	integrationconfig "actionlint.kjanat.dev/v2/internal/plugins/execution/configuration"

	"actionlint.kjanat.dev/v2/internal/plugins/execution/preparation"
)

func TestPrepareMapsGeneratedLines(t *testing.T) {
	t.Parallel()
	script := Prepare("#!/bin/bash\n# note\necho ok\n", "# shellcheck disable=SC2086\n", "set -e")
	if script.Text() != "#!/bin/bash\n# shellcheck disable=SC2086\n# note\nset -e\necho ok\n" {
		t.Fatalf("prepared: %q", script.Text())
	}
	source := func(line int) preparation.Line { return preparation.Line{Line: line, Origin: preparation.LineScript} }
	for row, want := range []preparation.Line{
		source(1),
		{Origin: preparation.LineDirective},
		source(2),
		{Origin: preparation.LineStartup},
		source(3),
		// The row just past the end may hold parser errors and exclusive ends.
		source(4),
		{Origin: preparation.LineDirective},
	} {
		if got := script.Line(row + 1); got != want {
			t.Errorf("row %d: %+v, want %+v", row+1, got, want)
		}
	}
	if empty := Prepare("", "", ""); empty.Text() != "\n" || empty.Line(1).Origin != preparation.LineDirective {
		t.Fatalf("empty script: %q", empty.Text())
	}
}

func TestInputKeepsDeclaredLanguageAndDirectiveCoordinates(t *testing.T) {
	t.Parallel()
	prepared, err := prepareShellcheck(t, Run{Input: true, Engine: true, Language: "sh", Settings: &integrationconfig.Settings{Prefix: "# shellcheck disable=SC2086\n"}}, "#!/bin/sh\n# shellcheck shell=ksh\necho ${{ '}}' }}\n")
	if err != nil {
		t.Fatal(err)
	}
	var options struct {
		Shell string `json:"shell"`
	}
	if err := json.Unmarshal(prepared.Config, &options); err != nil {
		t.Fatal(err)
	}
	dialect := options.Shell
	if dialect != "ksh" || prepared.Source != "#!/bin/sh\n# shellcheck disable=SC2086\n# shellcheck shell=ksh\necho ___________\n" {
		t.Fatalf("prepared %q as %q", prepared.Source, dialect)
	}
	if prepared.LineMap == nil || prepared.LineMap(2) != (preparation.Line{Origin: preparation.LineDirective}) || prepared.LineMap(4) != (preparation.Line{Line: 3, Origin: preparation.LineScript}) {
		t.Fatal("directive prefix changed source line ownership")
	}
	plain, err := prepareShellcheck(t, Run{Input: true, Engine: true, Language: LanguageDialect("sh")}, "echo input")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plain.Config, &options); err != nil {
		t.Fatal(err)
	}
	dialect = options.Shell
	if dialect != "sh" || plain.Source != "echo input\n" || plain.LineMap == nil {
		t.Fatalf("unconfigured input changed: %+v as %q", plain, dialect)
	}
	if LanguageDialect("python") != "" {
		t.Fatal("python input inferred a shell dialect")
	}
}
