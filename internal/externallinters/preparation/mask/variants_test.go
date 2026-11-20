package mask

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/externallinters/preparation"
)

func TestVariants(t *testing.T) {
	t.Parallel()
	choice := "${{ github.ref == 'x' && 'p' || 'q' }}"
	cases := "${{ case(github.ref == 'a', 'r', github.ref == 'b', 's', 't') }}"
	unknown := "${{ github.sha }}"
	text := "é " + choice + "\n" + cases + " " + unknown + "\n"
	substitution := func(template, value string, row, column, alternatives int) preparation.Substitution {
		return preparation.Substitution{Template: template, Value: value, Row: row, Column: column, Alternatives: alternatives}
	}
	want := []Variant{
		{Text: "é p\nr _________________\n", Substitutions: []preparation.Substitution{substitution(choice, "p", 1, 3, 2), substitution(cases, "r", 2, 1, 3)}},
		{Text: "é q\ns _________________\n", Substitutions: []preparation.Substitution{substitution(choice, "q", 1, 3, 2), substitution(cases, "s", 2, 1, 3)}},
		{Text: "é p\nt _________________\n", Substitutions: []preparation.Substitution{substitution(choice, "p", 1, 3, 2), substitution(cases, "t", 2, 1, 3)}},
	}
	if got := Variants(text); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestVariantsKeepRowsMasked(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"echo hi",
		"echo ${{ github.sha }}",
		"echo ${{\n'a' }}",
		"echo ${{ fromJSON('\"a\\nb\"') }}",
		"echo ${{ unfinished",
	} {
		if got := Variants(text); got != nil {
			t.Errorf("%q: got %#v", text, got)
		}
	}
}
