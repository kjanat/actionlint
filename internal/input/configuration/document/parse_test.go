package document

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, src  string
		key, value Location
	}{
		{"config.yaml", "repository:\n  runners: [gpu]\n", Location{"config.yaml", 2, 3}, Location{"config.yaml", 2, 12}},
		{"config.jsonc", "{\n // comment\n \"repository\": {\"runners\": [\"gpu\"],},\n}", Location{"config.jsonc", 3, 17}, Location{"config.jsonc", 3, 28}},
		{"config.toml", "[repository]\nrunners = [\"gpu\"]\n", Location{"config.toml", 2, 1}, Location{"config.toml", 2, 11}},
	}
	want := map[string]any{"repository": map[string]any{"runners": []any{"gpu"}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, err := Parse(tc.name, []byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(d.Value, want) {
				t.Fatalf("value %#v", d.Value)
			}
			if got := d.Locations["/repository/runners"]; got != tc.key {
				t.Errorf("key %+v", got)
			}
			if got := d.ValueLocations["/repository/runners"]; got != tc.value {
				t.Errorf("value location %+v", got)
			}
		})
	}
}

func TestDuplicateKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{{"x.json", `{"a":1,"a":2}`}, {"x.yaml", "a: 1\na: 2\n"}, {"x.toml", "a=1\na=2\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(tc.name, []byte(tc.src)); err == nil {
				t.Fatal("expected duplicate key error")
			}
		})
	}
}

func TestTOMLArrayTables(t *testing.T) {
	t.Parallel()
	d, err := Parse("x.toml", []byte("[[items]]\nname='one'\n[[items]]\nname='two'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Locations["/items/1/name"]; got.Line != 4 {
		t.Fatalf("location %+v", got)
	}
}

func TestNumberAndMarshal(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"x.json", "x.yaml", "x.toml"} {
		v := map[string]any{"number": json.Number("123")}
		data, err := Marshal(name, v)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Parse(name, data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d.Value, v) {
			t.Fatalf("%s: %#v", name, d.Value)
		}
	}
}

func TestJSONMultipleDocuments(t *testing.T) {
	t.Parallel()
	if _, err := Parse("x.json", []byte(`{} {}`)); err == nil || !strings.Contains(err.Error(), "one document") {
		t.Fatalf("error %v", err)
	}
}

func TestTOMLNestedPositions(t *testing.T) {
	t.Parallel()
	src := "[[groups]]\nname = 'one'\n[[groups.members]]\nname = 'a'\n[[groups]]\nname = 'two'\n[[groups.members]]\nname = 'b'\nsettings = { enabled = true }\n"
	d, err := Parse("x.toml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for path, line := range map[string]int{"/groups/0/members/0/name": 4, "/groups/1/members/0/name": 8, "/groups/1/members/0/settings/enabled": 9} {
		if got := d.Locations[path]; got.Line != line {
			t.Errorf("%s: %+v", path, got)
		}
	}
}

func TestYAMLMergeLocations(t *testing.T) {
	t.Parallel()
	src := "base: &base\n  value: 1\nitem:\n  value: 2\n  <<: *base\n"
	d, err := Parse("x.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Locations["/item/value"]; got.Line != 4 {
		t.Fatalf("location %+v", got)
	}
}

func TestNumbersAndDates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ file, src, key, want string }{{"x.json", `{"n":123456789012345678901234567890}`, "n", "123456789012345678901234567890"}, {"x.toml", "n=9223372036854775807", "n", "9223372036854775807"}, {"x.toml", "when=1979-05-27T07:32:00Z", "when", "1979-05-27T07:32:00Z"}} {
		d, err := Parse(tc.file, []byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		if got := d.Value.(map[string]any)[tc.key]; fmt.Sprint(got) != tc.want {
			t.Errorf("%s: %v", tc.file, got)
		}
	}
	if _, err := Parse("x.toml", []byte("n=9223372036854775808")); err == nil {
		t.Fatal("expected integer overflow error")
	}
	for _, tc := range []struct{ file, src string }{{"x.toml", "n=inf"}, {"x.yaml", "n: .inf"}} {
		d, err := Parse(tc.file, []byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		v, ok := d.Value.(map[string]any)["n"].(float64)
		if !ok || !math.IsInf(v, 1) {
			t.Fatalf("%s: %#v", tc.file, d.Value)
		}
	}
}

func TestNestedTOMLLocations(t *testing.T) {
	t.Parallel()
	src := "values = [[1], [2, { nested = [3] }]]\n[[items]]\nkey=1\n"
	d, err := Parse("x.toml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for p, col := range map[string]int{"/values": 10, "/values/0": 11, "/values/1": 16, "/values/1/1": 20, "/values/1/1/nested": 31} {
		if loc := d.ValueLocations[p]; loc.Column != col {
			t.Errorf("%s = %+v, want column %d", p, loc, col)
		}
	}
	if loc := d.ValueLocations["/items/0"]; loc.Line != 2 || loc.Column != 1 {
		t.Errorf("array table = %+v", loc)
	}
}

func TestTOMLMarshalIntegerOverflow(t *testing.T) {
	t.Parallel()
	_, err := Marshal("x.toml", map[string]any{"n": json.Number("123456789012345678901234567890")})
	if err == nil {
		t.Fatal("expected integer overflow error")
	}
}

func TestYAMLLargeIntegerRoundTrip(t *testing.T) {
	t.Parallel()
	v := map[string]any{"n": json.Number("123456789012345678901234567890")}
	data, err := Marshal("x.yaml", v)
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse("x.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Value, v) {
		t.Fatalf("%s: %#v", data, d.Value)
	}
}

func TestYAMLDecimalLeadingZero(t *testing.T) {
	t.Parallel()
	d, err := Parse("x.yaml", []byte("first: 017\nsecond: 0o17\nthird: 018\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"first": json.Number("17"), "second": json.Number("15"), "third": json.Number("18")}
	if !reflect.DeepEqual(d.Value, want) {
		t.Fatalf("got %#v, want %#v", d.Value, want)
	}
}

func TestRewriteYAMLPreservesMovedComments(t *testing.T) {
	t.Parallel()
	original := []byte("# head\nold: 1 # inline\n# other head\nother: keep\n# footer\n")
	parsed, err := Parse("x.yaml", original)
	if err != nil {
		t.Fatal(err)
	}
	value := map[string]any{"new": json.Number("1"), "other": "keep"}
	locations := map[string]Location{"/new": parsed.ValueLocations["/old"], "/other": parsed.ValueLocations["/other"]}
	result, err := RewriteYAML(original, value, locations)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{"# head", "# inline", "# other head", "# footer"} {
		if got := strings.Count(string(result), comment); got != 1 {
			t.Errorf("comment %q appears %d times:\n%s", comment, got, result)
		}
	}
	reparsed, err := Parse("x.yaml", result)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reparsed.Value, value) {
		t.Fatalf("values %#v", reparsed.Value)
	}
}

func TestRewriteYAMLRetainsAliasComments(t *testing.T) {
	t.Parallel()
	original := []byte("base: &shared\n  value: one # anchor value\ncopy: *shared # alias use\n")
	parsed, err := Parse("x.yaml", original)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RewriteYAML(original, parsed.Value, parsed.ValueLocations)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range []string{"# anchor value", "# alias use"} {
		if strings.Count(string(result), comment) != 1 {
			t.Errorf("comment %q lost or duplicated:\n%s", comment, result)
		}
	}
	again, err := Parse("x.yaml", result)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Value, parsed.Value) {
		t.Fatalf("values changed: %#v", again.Value)
	}
}
