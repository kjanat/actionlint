package names_test

import (
	"testing"

	"actionlint.kjanat.dev/v2/internal/model/workflows/names"
)

func TestInventoryDetachesNamesAndSeparatesCompleteness(t *testing.T) {
	t.Parallel()
	input := []string{"Build", "Build"}
	complete := names.NewInventory(input, true)
	incomplete := names.NewInventory(input, false)
	input[0], input[1] = "Changed", "Other"
	if !complete.Complete() || complete.Len() != 1 || !complete.Contains("Build") || complete.Contains("Changed") || complete.Contains("build") {
		t.Fatal("inventory changed with caller storage or lost case-sensitive names")
	}
	if !complete.Missing("Missing") || incomplete.Missing("Missing") || incomplete.Complete() || !incomplete.Contains("Build") {
		t.Fatal("known names were confused with a complete inventory")
	}
	var absent names.Inventory
	if absent.Complete() || absent.Missing("Anything") || absent.Len() != 0 {
		t.Fatal("absent inventory asserted that a producer is missing")
	}
}
