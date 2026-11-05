package schemapath

import (
	"strconv"
	"testing"
	"unicode/utf8"
)

func TestRender(t *testing.T) {
	root := Path{}
	for _, tc := range []struct {
		path            Path
		display, schema string
	}{
		{root, "", ""},
		{root.Key("runs").Key("pre-if"), "runs.pre-if", "runs.pre-if"},
		{root.Key("RUNS").Key("Steps").Index(12).Key("with").LooseKey("a.b"), "RUNS.Steps[12].with['a.b']", "runs.steps.*.with.*"},
		{root.LooseKey(".leading"), "['.leading']", "*"},
		{root.Key("outputs").LooseKey("0"), "outputs['0']", "outputs.*"},
		{root.Key("outputs").LooseKey("a\nb"), `outputs['a\nb']`, "outputs.*"},
		{root.Key("outputs").LooseKey(`a\nb`), `outputs['a\\nb']`, "outputs.*"},
		{root.Key("outputs").LooseKey("a b"), "outputs['a b']", "outputs.*"},
		{root.Key("outputs").LooseKey("a'b"), "outputs['a''b']", "outputs.*"},
		{root.Key("outputs").LooseKey(""), "outputs['']", "outputs.*"},
		{root.Key("outputs").LooseKey("a\u202eb"), `outputs['a\u202eb']`, "outputs.*"},
		{root.Key("outputs").LooseKey("a\u200bb"), `outputs['a\u200bb']`, "outputs.*"},
		{root.Key("outputs").LooseKey("a\ufeffb"), `outputs['a\ufeffb']`, "outputs.*"},
		{root.Key("outputs").LooseKey(`a"b`), `outputs['a"b']`, "outputs.*"},
		{root.Key("outputs").LooseKey("café日本"), "outputs['café日本']", "outputs.*"},
		{root.Key("outputs").LooseKey("*"), "outputs['*']", "outputs.*"},
		{root.Key("outputs").LooseKey("MixedCase").Key("value"), "outputs.MixedCase.value", "outputs.*.value"},
	} {
		if got := tc.path.Display(); got != tc.display {
			t.Errorf("Display() = %q, want %q", got, tc.display)
		}
		if got := tc.path.Schema(); got != tc.schema {
			t.Errorf("Schema() = %q, want %q", got, tc.schema)
		}
	}
}

func TestPathBranchesAreIndependent(t *testing.T) {
	base := Path{}.Key("runs").Key("steps").Index(0).Key("with")
	first := base.LooseKey("first")
	second := base.LooseKey("second")
	if got := first.Display(); got != "runs.steps[0].with.first" {
		t.Fatal(got)
	}
	if got := second.Display(); got != "runs.steps[0].with.second" {
		t.Fatal(got)
	}
	if got := base.Display(); got != "runs.steps[0].with" {
		t.Fatal(got)
	}
}

func FuzzDisplay(f *testing.F) {
	for _, pair := range [][2]string{
		{"x\ny", `x\ny`},
		{"a.b", "a['b']"},
		{"a'b", "a''b"},
		{"p\u202eq", `p\u202eq`},
		{"café日本", ""},
	} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if !utf8.ValidString(a) || !utf8.ValidString(b) {
			t.Skip()
		}
		first := Path{}.Key("outputs").LooseKey(a).Key("value").Display()
		second := Path{}.Key("outputs").LooseKey(b).Key("value").Display()
		if a != b && first == second {
			t.Fatalf("keys %q and %q both render as %q", a, b, first)
		}
		for _, rendered := range []string{first, second} {
			for _, r := range rendered {
				if !strconv.IsPrint(r) {
					t.Fatalf("non-printable rune %U in %q", r, rendered)
				}
			}
		}
	})
}
