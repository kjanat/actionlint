package names_test

import (
	"slices"
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

func TestDeclareSeparatesDisplayedAndMatchedNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc, value       string
		absent            bool
		known             bool
		explicit, literal string
		hasExplicit       bool
		hasLiteral        bool
	}{
		{desc: "absent", absent: true, known: true},
		{desc: "empty", value: "", known: true},
		{desc: "plain", value: "CI", known: true, explicit: "CI", hasExplicit: true, literal: "CI", hasLiteral: true},
		{desc: "literal expression", value: "${{ 'CI' }}", known: true, explicit: "CI", hasExplicit: true},
		{desc: "empty literal expression", value: "${{ '' }}", known: true},
		{desc: "dynamic", value: "CI ${{ github.ref }}"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			var name *definition.String
			if !tc.absent {
				name = &definition.String{Value: tc.value}
			}
			declared := names.Declare(name)
			explicit, hasExplicit := declared.Explicit()
			literal, hasLiteral := declared.Literal()
			if declared.Known() != tc.known || hasExplicit != tc.hasExplicit || hasLiteral != tc.hasLiteral ||
				hasExplicit && explicit != tc.explicit || hasLiteral && literal != tc.literal {
				t.Fatalf("known=%v explicit=%q,%v literal=%q,%v", declared.Known(), explicit, hasExplicit, literal, hasLiteral)
			}
		})
	}
	if names.Unknown().Known() {
		t.Fatal("unknown name reported as known")
	}
}

func TestInventorySharingNameIgnoresCaseAndExcludedPaths(t *testing.T) {
	t.Parallel()
	recorded := []names.NamedWorkflow{{Path: "a.yml", Name: "CI"}, {Path: "b.yml", Name: "ci"}, {Path: "c.yml", Name: "CI lint"}}
	inventory := names.NewInventory([]string{"CI", "ci", "CI lint"}, true).WithLiteralNames(recorded)
	recorded[0].Name = "Changed"
	if got := inventory.SharingName("Ci"); !slices.Equal(got, []names.NamedWorkflow{{Path: "a.yml", Name: "CI"}, {Path: "b.yml", Name: "ci"}}) {
		t.Fatalf("sharing = %v", got)
	}
	others := inventory.ExcludingLiteral(func(path string) bool { return path == "a.yml" })
	if got := others.SharingName("CI"); !slices.Equal(got, []names.NamedWorkflow{{Path: "b.yml", Name: "ci"}}) {
		t.Fatalf("excluded sharing = %v", got)
	}
	if len(inventory.SharingName("CI")) != 2 || !others.Contains("CI") || !others.Complete() {
		t.Fatal("exclusion changed the original inventory or its filter names")
	}
}
