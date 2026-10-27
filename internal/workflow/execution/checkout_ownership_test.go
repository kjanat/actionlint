package execution_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/workflow/execution"
)

func TestCheckoutPlacementOwnsValuesAndImmutableLinks(t *testing.T) {
	input := execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "parent"}, CaseInsensitive: true}
	parent := execution.NewCheckoutPlacement(input, execution.CheckoutPlacement{})
	child := execution.NewCheckoutPlacement(execution.Checkout{Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: "child"}}, parent)
	input.Directory.Path, input.Foreign = "changed input", true
	details := parent.Details()
	details.Directory.Path, details.Redirected = "changed details", true
	previous := child.Previous()
	previous = execution.NewCheckoutPlacement(details, previous)
	if parent.Details().Directory.Path != "parent" || parent.Details().Foreign || parent.Details().Redirected || !parent.Details().CaseInsensitive {
		t.Fatal("caller changed published placement values")
	}
	if child.Previous() != parent || previous == parent || child.Matching("parent/script.sh") != parent {
		t.Fatal("replacing a detached handle changed the original chain")
	}
	merged := execution.MergeCheckoutBranches(parent, child, false)
	if merged == child || merged.Details().Directory.Kind != execution.DirectoryUnknown || merged.Previous() != parent {
		t.Fatal("conditional branch did not build an independent result")
	}
	if child.Details().Directory.Kind != execution.DirectoryKnown || parent.Details().Directory.Kind != execution.DirectoryKnown {
		t.Fatal("branch merge mutated an input placement")
	}
}

func TestEmptyCheckoutPlacementHasNoMatch(t *testing.T) {
	var empty execution.CheckoutPlacement
	if empty.Present() || empty.Previous().Present() || empty.Matching("file").Present() || empty.SameSelfCheckout(".") {
		t.Fatal("empty handle fabricated a placement")
	}
	if _, matched := empty.Relative("file"); matched {
		t.Fatal("empty handle matched a path")
	}
}

func TestCheckoutForUsesTheMatchedPrefixLength(t *testing.T) {
	for _, tc := range []struct{ placement, requested, want string }{
		{"discarded/../s", "S/a", "S"},
		{"./repo/./", "REPO/script", "REPO"},
		{"repo/..", "script", "."},
	} {
		t.Run(tc.placement, func(t *testing.T) {
			paths := execution.Paths{Placements: execution.NewCheckoutPlacement(execution.Checkout{
				Directory: execution.Directory{Kind: execution.DirectoryKnown, Path: tc.placement}, CaseInsensitive: true,
			}, execution.CheckoutPlacement{})}
			got, known := paths.CheckoutFor(tc.requested)
			if !known || got != tc.want {
				t.Fatalf("checkout=%q known=%t, want %q", got, known, tc.want)
			}
		})
	}
}
