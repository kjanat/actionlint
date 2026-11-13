package suppression

import (
	"strings"
	"testing"

	rule "actionlint.kjanat.dev/v2/internal/checking/rules"

	sourcemodel "actionlint.kjanat.dev/v2/internal/input/source"
	sourceposition "actionlint.kjanat.dev/v2/internal/input/source/position"
)

func TestRemoveSuppressionSelectors(t *testing.T) {
	t.Parallel()
	allowed := []string{"expression", "shellcheck", "matrix"}
	for _, tc := range []struct {
		name, source, want string
		remove             []string
	}{
		{"last inline", "run: echo ok # ignore expression\n", "run: echo ok\n", []string{"expression"}},
		{"all bracketed", "run: echo ok # ignore[expression, shellcheck] -- obsolete\n", "run: echo ok\n", []string{"expression", "shellcheck"}},
		{"partial", "run: echo ok # actionlint:ignore expression,shellcheck -- keep reason\n", "run: echo ok # actionlint:ignore shellcheck -- keep reason\n", []string{"expression"}},
		{"mixed separators", "run: echo ok # ignore[expression, shellcheck matrix]\n", "run: echo ok # ignore[expression matrix]\n", []string{"shellcheck"}},
		{"duplicate selectors", "run: echo ok # actionlint-ignore expression expression,shellcheck\n", "run: echo ok # actionlint-ignore shellcheck\n", []string{"expression"}},
		{"bracket whitespace", "run: echo ok # ignore[ expression, shellcheck  ] -- café\n", "run: echo ok # ignore[ shellcheck  ] -- café\n", []string{"expression"}},
		{"tabs", "run: echo ok # ignore\texpression\tshellcheck\n", "run: echo ok # ignore\tshellcheck\n", []string{"expression"}},
		{"next line", "# actionlint-ignore-next-line expression\nrun: echo ok\n", "run: echo ok\n", []string{"expression"}},
		{"partial next line", "# ignore-next-line expression shellcheck -- keep\nrun: echo ok\n", "# ignore-next-line shellcheck -- keep\nrun: echo ok\n", []string{"expression"}},
		{"do not activate preceding directive", "# ignore-next-line expression\n# ignore-next-line shellcheck\nrun: echo ok\n", "# ignore-next-line expression\n\nrun: echo ok\n", []string{"shellcheck"}},
		{"block header", "run: | # ignore expression\n  echo ok\n", "run: |\n  echo ok\n", []string{"expression"}},
		{"quoted hash and unicode", "run: 'echo café # keep' # ignore expression", "run: 'echo café # keep'", []string{"expression"}},
		{"malformed YAML", "jobs: [ # ignore expression\n", "jobs: [\n", []string{"expression"}},
		{"BOM", "\ufeffrun: echo ok # ignore expression\n", "\ufeffrun: echo ok\n", []string{"expression"}},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			t.Run(tc.name+ending, func(t *testing.T) {
				t.Parallel()
				content := []byte(strings.ReplaceAll(tc.source, "\n", ending))
				directives, invalid := Parse(content, allowed, rule.Names{})
				if len(invalid) != 0 || len(directives) != 1 {
					t.Fatalf("invalid fixture: %+v %+v", directives, invalid)
				}
				edit, ok := Remove(content, directives[0].Position, tc.remove, allowed, rule.Names{})
				if !ok {
					t.Fatal("no removal edit")
				}
				first, last, err := sourceposition.Range(content, sourcemodel.YAML, edit.Start, edit.End)
				if err != nil {
					t.Fatal(err)
				}
				got := string(content[:first]) + edit.Replacement + string(content[last:])
				if want := strings.ReplaceAll(tc.want, "\n", ending); got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			})
		}
	}
}
