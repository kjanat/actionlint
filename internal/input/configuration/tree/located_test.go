package tree

import (
	"reflect"
	"slices"
	"strconv"
	"testing"
)

func TestLocatedOverlayPreservesKeyedOrigins(t *testing.T) {
	t.Parallel()
	policy := func(id string) any { return map[string]any{"id": id} }
	base := Capture(map[string]any{"policies": []any{policy("a"), policy("b")}}, map[string]Input{"/policies/0/id": {File: "base"}, "/policies/1/id": {File: "base"}})
	patch := Capture(map[string]any{"policies": []any{policy("b"), policy("c")}}, map[string]Input{"/policies/0/id": {File: "patch"}, "/policies/1/id": {File: "patch"}})
	got := Overlay(base, patch, Plan{"/policies": {Mode: KeyedList, Key: "id"}})
	vals := got.Value().(map[string]any)["policies"].([]any)
	origins := got.Origins()
	for i, want := range []struct{ id, file string }{{"a", "base"}, {"b", "patch"}, {"c", "patch"}} {
		if vals[i].(map[string]any)["id"] != want.id || origins["/policies/"+strconv.Itoa(i)+"/id"].File != want.file {
			t.Fatalf("values=%v origins=%v", vals, origins)
		}
	}
}

func TestAtomicMembersAfterNullRetainsReset(t *testing.T) {
	t.Parallel()
	base := Capture(map[string]any{"facts": nil}, map[string]Input{"/facts": {File: "reset"}})
	patch := Capture(map[string]any{"facts": map[string]any{"one": "value"}}, map[string]Input{"/facts": {File: "patch"}, "/facts/one": {File: "patch"}})
	got := Overlay(base, patch, Plan{"/facts": {Mode: AtomicMembers, Members: []string{"one", "two"}}})
	facts := got.Value().(map[string]any)["facts"].(map[string]any)
	origins := got.Origins()
	if facts["one"] != "value" || facts["two"] != nil || origins["/facts"].Reset == nil || origins["/facts"].Reset.File != "reset" || origins["/facts/two"].File != "reset" {
		t.Fatalf("facts=%v origins=%v", facts, origins)
	}
}

func TestCompilePlanFromOwningFields(t *testing.T) {
	t.Parallel()
	type policy struct {
		ID string `yaml:"id"`
	}
	type nested struct {
		Policies []policy `yaml:"policies" merge:"key=id"`
	}
	type root struct {
		Nested nested        `yaml:"nested"`
		Facts  planTestFacts `yaml:"facts" merge:"atomic-members"`
	}
	plan, err := CompilePlan(reflect.TypeFor[root]())
	if err != nil {
		t.Fatal(err)
	}
	if plan["/nested/policies"].Mode != KeyedList || plan["/nested/policies"].Key != "id" || plan["/facts"].Mode != AtomicMembers || !slices.Equal(plan["/facts"].Members, []string{"first", "second"}) {
		t.Fatalf("plan = %+v", plan)
	}
}

type planTestFacts struct{}

func (planTestFacts) MergeWire() reflect.Type {
	return reflect.TypeFor[struct {
		First  string `yaml:"first"`
		Second string `yaml:"second"`
	}]()
}

func TestCompilePlanRejectsNonStringKey(t *testing.T) {
	t.Parallel()
	type invalidItem struct {
		ID int `yaml:"id"`
	}
	type invalidRoot struct {
		Items []invalidItem `yaml:"items" merge:"key=id"`
	}
	if _, err := CompilePlan(reflect.TypeFor[invalidRoot]()); err == nil {
		t.Fatal("accepted a numeric keyed-list ID")
	}
	type itemID string
	type validItem struct {
		ID itemID `yaml:"id"`
	}
	type validRoot struct {
		Items []validItem `yaml:"items" merge:"key=id"`
	}
	if _, err := CompilePlan(reflect.TypeFor[validRoot]()); err != nil {
		t.Fatalf("rejected named string ID: %v", err)
	}
}

func TestReplaceModeReplacesObjectAndOrigins(t *testing.T) {
	t.Parallel()
	type root struct {
		Settings map[string]string `yaml:"settings" merge:"replace"`
	}
	plan, err := CompilePlan(reflect.TypeFor[root]())
	if err != nil {
		t.Fatal(err)
	}
	base := Capture(map[string]any{"settings": map[string]any{"old": "value"}}, map[string]Input{"/settings/old": {File: "base"}})
	patch := Capture(map[string]any{"settings": map[string]any{"new": "value"}}, map[string]Input{"/settings/new": {File: "overlay"}})
	got := Overlay(base, patch, plan)
	values := got.Value().(map[string]any)["settings"].(map[string]any)
	origins := got.Origins()
	if len(values) != 1 || values["new"] != "value" || origins["/settings/new"].File != "overlay" {
		t.Fatalf("values=%v origins=%v", values, origins)
	}
	if _, ok := origins["/settings/old"]; ok {
		t.Fatalf("replaced origin retained: %v", origins)
	}
}
