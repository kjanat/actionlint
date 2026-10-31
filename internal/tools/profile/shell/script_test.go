package shell

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/tools/profile"
)

func TestPrepareMapsGeneratedLines(t *testing.T) {
	script := Prepare("#!/bin/bash\n# note\necho ok\n", "# shellcheck disable=SC2086\n", "set -e")
	if script.Text() != "#!/bin/bash\n# shellcheck disable=SC2086\n# note\nset -e\necho ok\n" {
		t.Fatalf("prepared: %q", script.Text())
	}
	source := func(line int) profile.Line { return profile.Line{Line: line, Origin: profile.LineScript} }
	for row, want := range []profile.Line{
		source(1),
		{Origin: profile.LineDirective},
		source(2),
		{Origin: profile.LineStartup},
		source(3),
		// The row just past the end may hold parser errors and exclusive ends.
		source(4),
		{Origin: profile.LineDirective},
	} {
		if got := script.Line(row + 1); got != want {
			t.Errorf("row %d: %+v, want %+v", row+1, got, want)
		}
	}
	if empty := Prepare("", "", ""); empty.Text() != "\n" || empty.Line(1).Origin != profile.LineDirective {
		t.Fatalf("empty script: %q", empty.Text())
	}
}

func TestInputKeepsDeclaredLanguageAndDirectiveCoordinates(t *testing.T) {
	prepared, dialect := Input("#!/bin/sh\n# shellcheck shell=ksh\necho ${{ '}}' }}\n", "sh", "# shellcheck disable=SC2086\n")
	if dialect != "ksh" || prepared.Source != "#!/bin/sh\n# shellcheck disable=SC2086\n# shellcheck shell=ksh\necho ___________\n" {
		t.Fatalf("prepared %q as %q", prepared.Source, dialect)
	}
	if prepared.LineMap == nil || prepared.LineMap(2) != (profile.Line{Origin: profile.LineDirective}) || prepared.LineMap(4) != (profile.Line{Line: 3, Origin: profile.LineScript}) {
		t.Fatal("directive prefix changed source line ownership")
	}
	plain, dialect := Input("echo input", LanguageDialect("sh"), "")
	if dialect != "sh" || plain.Source != "echo input" || plain.LineMap != nil {
		t.Fatalf("unconfigured input changed: %+v as %q", plain, dialect)
	}
	if LanguageDialect("python") != "" {
		t.Fatal("python input inferred a shell dialect")
	}
}
