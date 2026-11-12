package output_test

import (
	"bytes"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/checking"
	"actionlint.kjanat.dev/v2/internal/integrations/buildinfo"
	render "actionlint.kjanat.dev/v2/internal/output"
)

func TestErrorPrintAllKinds(t *testing.T) {
	t.Parallel()
	f, err := render.NewFormatter("{{range $ = allKinds}}{{$.Name}}: {{$.Description}}\n{{end}}", buildinfo.Version, checking.ListRules())
	if err != nil {
		t.Fatal(err)
	}

	f.RegisterRule(render.Rule{Name: "rule1", Description: "description for rule1"})
	f.RegisterRule(render.Rule{Name: "rule2", Description: "description for rule2"})

	var b bytes.Buffer
	if err := f.Print(&b, nil); err != nil {
		t.Fatal(err)
	}
	output := b.String()

	for _, want := range []string{
		"syntax-check: Validate GitHub Actions workflow syntax\n",
		"inline-suppression: Validate inline suppression directives\n",
		"forbidden-suppression: Report suppressions forbidden by policy\n",
		"rule1: description for rule1\n",
		"rule2: description for rule2\n",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("%q is not included in `allKinds` output: %q", want, output)
		}
	}
}
