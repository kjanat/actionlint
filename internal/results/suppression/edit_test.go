package suppression

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"

	sourcemodel "actionlint.kjanat.dev/v2/internal/input/source"
	sourceposition "actionlint.kjanat.dev/v2/internal/input/source/position"
)

func TestSuppressionEdit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, want string
		line               int
	}{
		{"plain", "run: echo ok", "# actionlint-ignore-next-line expression\nrun: echo ok", 1},
		{"comment", "run: echo ok # keep me", "# actionlint-ignore-next-line expression\nrun: echo ok # keep me", 1},
		{"block", "run: |\n  echo ok\nnext: value", "# actionlint-ignore-next-line expression\nrun: |\n  echo ok\nnext: value", 2},
		{"quoted", "name: \"first\n  second\"", "# actionlint-ignore-next-line expression\nname: \"first\n  second\"", 2},
		{"existing", "run: | # actionlint:ignore shellcheck -- reviewed\n  echo ok", "run: | # ignore[shellcheck, expression] -- reviewed\n  echo ok", 2},
		{"bracket", "run: echo ok # ignore[shellcheck]", "run: echo ok # ignore[shellcheck, expression]", 1},
		{"unprefixed", "run: echo ok # ignore shellcheck", "run: echo ok # ignore[shellcheck, expression]", 1},
		{"preceding", "# actionlint-ignore-next-line shellcheck -- reviewed\nrun: |\n  echo ok", "# actionlint-ignore-next-line shellcheck, expression -- reviewed\nrun: |\n  echo ok", 3},
		{"unicode", "name: café\nrun: echo 😀", "name: café\n# actionlint-ignore-next-line expression\nrun: echo 😀", 2},
		{"malformed YAML", "jobs: [", "# actionlint-ignore-next-line expression\njobs: [", 1},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+"/"+ending, func(t *testing.T) {
				t.Parallel()
				content := []byte(strings.ReplaceAll(tc.source, "\n", ending))
				edit, ok := Edit(content, tc.line, "expression", []string{"expression", "shellcheck"}, rule.Names{})
				if !ok {
					t.Fatal("no suppression edit")
				}
				first, last, err := sourceposition.Range(content, sourcemodel.YAML, edit.Start, edit.End)
				if err != nil {
					t.Fatal(err)
				}
				got := string(content[:first]) + edit.Replacement + string(content[last:])
				want := strings.ReplaceAll(tc.want, "\n", ending)
				// A single unterminated line has no line-ending preference.
				if !strings.Contains(tc.source, "\n") {
					want = tc.want
				}
				if got != want {
					t.Fatalf("got %q; want %q", got, want)
				}
			})
		}
	}
	for _, content := range []string{
		"run: echo ok # ignore[expression]",
		"# actionlint-ignore-next-line unknown\nrun: echo ok",
	} {
		if edit, ok := Edit([]byte(content), strings.Count(content, "\n")+1, "expression", []string{"expression", "shellcheck"}, rule.Names{}); ok {
			t.Fatalf("edit hides an existing directive: %+v", edit)
		}
	}
}
