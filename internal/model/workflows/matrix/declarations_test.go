package matrix

import (
	"maps"
	"testing"

	value "actionlint.kjanat.dev/v2/internal/model/contexts/values"
)

func TestDeclarationQueriesDetachReturnedStorage(t *testing.T) {
	t.Parallel()
	resolution := resolveSemanticMatrix(value.Template(`${{ fromJSON('{"os":["linux","windows"],"include":[{"extra":"added"}],"exclude":[{"os":"windows"}]}') }}`, value.Origin{}))
	axes := resolution.Axes()
	if len(axes) != 1 || len(axes[0].Values) != 2 || !axes[0].Complete {
		t.Fatalf("unexpected declared axes: %#v", axes)
	}
	axes[0].Name = "changed"
	axes[0].Values[0] = value.FromJSON("changed", value.Origin{})
	axis, found := resolution.Axis("OS")
	if !found || axis.Name != "os" || !axis.Values[0].Equal(value.FromJSON("linux", value.Origin{})) {
		t.Fatalf("returned axes changed stored declarations: %#v", axis)
	}
	axis.Values[0] = value.FromJSON("also changed", value.Origin{})
	if fresh := resolution.Axes()[0]; !fresh.Values[0].Equal(value.FromJSON("linux", value.Origin{})) {
		t.Fatal("single-axis lookup exposed stored values")
	}
	if _, found := resolution.Axis("extra"); found {
		t.Fatal("include-only field was reported as a base axis")
	}
	for _, query := range []func() []Combination{resolution.Inclusions, resolution.Exclusions} {
		filters := query()
		if len(filters) != 1 {
			t.Fatalf("unexpected declared filters: %#v", filters)
		}
		filters[0] = Combination{}
		if fresh := query(); len(maps.Collect(fresh[0].Values())) != 1 {
			t.Fatal("returned filters changed stored declarations")
		}
	}
}
