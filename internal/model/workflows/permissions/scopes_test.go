package permissions

import "testing"

func TestScopesIsACopy(t *testing.T) {
	t.Parallel()
	first := Scopes()
	first["contents"][0] = "mutated"
	delete(first, "issues")
	second := Scopes()
	if second["contents"][0] != "read" || len(second["issues"]) != 3 || len(second) != len(scopes) {
		t.Fatalf("catalogue aliased: %+v", second)
	}
}
