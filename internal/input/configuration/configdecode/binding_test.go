package configdecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type namedLocations []string

func (n *namedLocations) DecodeConfig(value any) error {
	if scalar, ok := value.(string); ok {
		*n = []string{scalar}
		return nil
	}
	type plain namedLocations
	var values plain
	if err := Decode(value, &values); err != nil {
		return err
	}
	if len(values) == 0 {
		return errors.New("locations must not be empty")
	}
	*n = namedLocations(values)
	return nil
}

func TestDecodeCallsSharedHooksForScalarArrayAndNull(t *testing.T) {
	t.Parallel()
	type options struct {
		Locations namedLocations `yaml:"require-at"`
	}
	for _, input := range []any{"workflow", []any{"workflow"}} {
		var selected options
		if err := Decode(map[string]any{"require-at": input}, &selected); err != nil {
			t.Fatal(err)
		}
		if len(selected.Locations) != 1 || selected.Locations[0] != "workflow" {
			t.Fatalf("locations: %v", selected.Locations)
		}
	}
	for _, input := range []any{nil, []any{}, []any{true}, map[string]any{}} {
		selected := options{Locations: namedLocations{"keep"}}
		err := Decode(map[string]any{"require-at": input}, &selected)
		if err == nil {
			t.Fatalf("accepted %#v", input)
		}
		if selected.Locations[0] != "keep" {
			t.Fatal("failed decode changed destination")
		}
		failure, ok := errors.AsType[*Error](err)
		if !ok || failure.Path != "/require-at" && failure.Path != "/require-at/0" {
			t.Fatalf("error path: %v", err)
		}
	}
}

func TestDecodeKeepsStrictTypesAndExactMapPaths(t *testing.T) {
	t.Parallel()
	type entry struct {
		Enabled bool `yaml:"enabled"`
	}
	type config struct {
		Entries map[string]entry `yaml:"entries"`
	}
	var selected config
	err := Decode(map[string]any{"entries": map[string]any{"a.b/c~d": map[string]any{"enabled": "true"}}}, &selected)
	failure, ok := errors.AsType[*Error](err)
	if !ok || failure.Path != "/entries/a.b~1c~0d/enabled" {
		t.Fatalf("error path: %v", err)
	}
	if err := Decode(map[string]any{"unknown": true}, &selected); err == nil {
		t.Fatal("accepted an unknown key")
	}
	var names []string
	if err := Decode([]any{json.Number("1")}, &names); err == nil {
		t.Fatal("accepted a number as a string")
	}
}

func TestDecodePointerToCustomValues(t *testing.T) {
	t.Parallel()
	type options struct {
		Locations *namedLocations `yaml:"locations"`
	}
	var selected options
	if err := Decode(map[string]any{"locations": []any{"workflow"}}, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.Locations == nil || len(*selected.Locations) != 1 || (*selected.Locations)[0] != "workflow" {
		t.Fatalf("pointer selection: %#v", selected.Locations)
	}
	if err := Decode(map[string]any{"locations": nil}, &selected); err != nil || selected.Locations != nil {
		t.Fatalf("null pointer: %#v %v", selected.Locations, err)
	}
}

func TestEncodePreservesEmptyAndNullContainers(t *testing.T) {
	t.Parallel()
	value, err := Encode(map[string]any{"empty": []string{}, "null": []string(nil), "false": false})
	if err != nil {
		t.Fatal(err)
	}
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("object: %#v", value)
	}
	if object["null"] != nil || object["false"] != false {
		t.Fatalf("values: %#v", object)
	}
	if list, ok := object["empty"].([]any); !ok || len(list) != 0 {
		t.Fatalf("empty: %#v", object["empty"])
	}
}

func TestNumbersKeepExactRuntimeValues(t *testing.T) {
	t.Parallel()
	var selected map[string]any
	input := map[string]any{"count": json.Number("3"), "half": json.Number("0.5"), "huge": json.Number("123456789012345678901234567890"), "tenth": json.Number("0.1")}
	if err := Decode(input, &selected); err != nil {
		t.Fatal(err)
	}
	if selected["count"] != 3 || selected["half"] != 0.5 || selected["huge"] != input["huge"] || selected["tenth"] != input["tenth"] {
		t.Fatalf("runtime numbers: %#v", selected)
	}
	var count int
	if err := Decode(json.Number("3.0"), &count); err != nil || count != 3 {
		t.Fatalf("whole decimal: %v %v", count, err)
	}
	if err := Decode(json.Number("3.5"), &count); err == nil {
		t.Fatal("accepted a fractional integer")
	}
	var small int8
	if err := Decode(json.Number("300"), &small); err == nil {
		t.Fatal("accepted integer overflow")
	}
}

func TestErrorTraversalKeepsSiblingPaths(t *testing.T) {
	t.Parallel()
	first := errors.New("first failure")
	second := errors.New("second failure")
	nested := fmt.Errorf("decoder wrapper: %w", errors.Join(
		&Error{Path: "/first", Cause: first},
		fmt.Errorf("inner wrapper: %w", &Error{Path: "/second", Cause: second}),
	))
	err := At("options", decodeError(nested, nil))
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("lost error causes: %v", err)
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 2 || lines[0] != "/options/first: first failure" || lines[1] != "/options/second: second failure" {
		t.Fatalf("paths: %v", err)
	}
}

func TestEncodePreservesNumericTypesAndBounds(t *testing.T) {
	t.Parallel()
	type namedInt int32
	values := []any{int(-3), int8(-128), int16(-32768), int32(-2147483648), int64(-9223372036854775808), uint(3), uint8(255), uint16(65535), uint32(4294967295), uint64(18446744073709551615), float32(0.1), float64(0.1), namedInt(-9), json.Number("123456789012345678901234567890")}
	for _, value := range values {
		encoded, err := Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.TypeOf(encoded) != reflect.TypeOf(value) || !reflect.DeepEqual(encoded, value) {
			t.Fatalf("Encode(%T(%v)) = %T(%v)", value, value, encoded, encoded)
		}
	}
}

func TestEncodePartialPreservesExplicitMapValues(t *testing.T) {
	t.Parallel()
	type nested struct {
		Enabled *bool          `yaml:"enabled"`
		Options map[string]any `yaml:"options"`
	}
	input := struct {
		Nil   *string  `yaml:"nil"`
		Empty []string `yaml:"empty"`
		Tool  *nested  `yaml:"tool"`
	}{Empty: []string{}, Tool: &nested{Options: map[string]any{"unknown": nil, "limit": int(3)}}}
	encoded, err := EncodePartial(input)
	if err != nil {
		t.Fatal(err)
	}
	object := encoded.(map[string]any)
	if _, ok := object["nil"]; ok {
		t.Fatal("unset pointer was encoded")
	}
	if list, ok := object["empty"].([]any); !ok || len(list) != 0 {
		t.Fatalf("empty list lost: %#v", object)
	}
	tool := object["tool"].(map[string]any)
	if _, ok := tool["enabled"]; ok {
		t.Fatal("unset nested field was encoded")
	}
	options := tool["options"].(map[string]any)
	if value, ok := options["unknown"]; !ok || value != nil {
		t.Fatalf("explicit null map entry lost: %#v", options)
	}
	if value, ok := options["limit"].(int); !ok || value != 3 {
		t.Fatalf("numeric map entry changed: %#v", options)
	}
}
