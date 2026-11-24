package codec_test

import (
	"fmt"
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	upstream "github.com/kjanat/go-yaml/v4"
)

type prefilledChild struct {
	Called bool
	Value  string
}

type codecPlainStruct struct {
	Name    string `yaml:"name"`
	private string
}

type upstreamPlainStruct struct {
	Name    string `yaml:"name"`
	private string
}

type codecPlainContainers struct {
	Map   map[string]*codecPlainStruct `yaml:"map"`
	Slice []*codecPlainStruct          `yaml:"slice"`
	Empty []*codecPlainStruct          `yaml:"empty"`
	Other string                       `yaml:"other"`
}

type upstreamPlainContainers struct {
	Map   map[string]*upstreamPlainStruct `yaml:"map"`
	Slice []*upstreamPlainStruct          `yaml:"slice"`
	Empty []*upstreamPlainStruct          `yaml:"empty"`
	Other string                          `yaml:"other"`
}

func sliceBacking[T any](values []T) *T {
	if cap(values) == 0 {
		return nil
	}
	return &values[:cap(values)][0]
}

func TestPrefilledPlainContainersKeepAbsentValuesAndIdentity(t *testing.T) {
	t.Parallel()
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
			t.Parallel()
			const source = "other: decoded\n"
			originalEntry := &codecPlainStruct{Name: "kept", private: "owned-private"}
			originalSliceEntry := &codecPlainStruct{Name: "slice-kept", private: "slice-private"}
			originalSlice := make([]*codecPlainStruct, 1, 3)
			originalSlice[0] = originalSliceEntry
			originalBacking := sliceBacking(originalSlice)
			originalEmpty := make([]*codecPlainStruct, 0, 3)
			originalEmptyBacking := sliceBacking(originalEmpty)
			got := codecPlainContainers{Map: map[string]*codecPlainStruct{"entry": originalEntry}, Slice: originalSlice, Empty: originalEmpty, Other: "before"}

			baselineEntry := &upstreamPlainStruct{Name: "kept", private: "owned-private"}
			baselineSliceEntry := &upstreamPlainStruct{Name: "slice-kept", private: "slice-private"}
			baselineSlice := make([]*upstreamPlainStruct, 1, 3)
			baselineSlice[0] = baselineSliceEntry
			baselineBacking := sliceBacking(baselineSlice)
			baselineEmpty := make([]*upstreamPlainStruct, 0, 3)
			baselineEmptyBacking := sliceBacking(baselineEmpty)
			want := upstreamPlainContainers{Map: map[string]*upstreamPlainStruct{"entry": baselineEntry}, Slice: baselineSlice, Empty: baselineEmpty, Other: "before"}

			if err := decodePrefilledSource(t, []byte(source), &got, selected); err != nil {
				t.Fatalf("Cursor.Decode: %v", err)
			}
			if err := upstream.Unmarshal([]byte(source), &want); err != nil {
				t.Fatalf("upstream.Unmarshal baseline: %v", err)
			}
			if got.Other != want.Other || got.Other != "decoded" {
				t.Errorf("decoded field = %q, upstream = %q; want decoded", got.Other, want.Other)
			}
			if want.Map["entry"] != baselineEntry || want.Map["entry"].private != originalEntry.private {
				t.Fatalf("upstream map baseline changed entry identity/state: %#v", want.Map["entry"])
			}
			if entry := got.Map["entry"]; entry != originalEntry || entry == nil || entry.private != want.Map["entry"].private {
				t.Errorf("absent map entry = %#v (%p), want original %#v (%p) and upstream private state %q", got.Map["entry"], got.Map["entry"], originalEntry, originalEntry, want.Map["entry"].private)
			}
			if len(got.Slice) != 1 || got.Slice[0] != originalSliceEntry || got.Slice[0].private != originalSliceEntry.private {
				t.Errorf("absent slice entry = %#v, want original pointer %#v", got.Slice, originalSliceEntry)
			}
			if len(want.Slice) != 1 || want.Slice[0] != baselineSliceEntry || sliceBacking(want.Slice) != baselineBacking {
				t.Fatalf("upstream slice baseline changed entry/header identity: %#v", want.Slice)
			}
			if cap(got.Slice) != cap(want.Slice) || cap(got.Slice) != 3 || sliceBacking(got.Slice) != originalBacking {
				t.Errorf("absent slice header = len %d cap %d backing %p; upstream len %d cap %d; want original backing %p", len(got.Slice), cap(got.Slice), sliceBacking(got.Slice), len(want.Slice), cap(want.Slice), originalBacking)
			}
			if len(got.Empty) != 0 || cap(got.Empty) != cap(want.Empty) || cap(got.Empty) != 3 || sliceBacking(got.Empty) != originalEmptyBacking {
				t.Errorf("absent empty slice header = len %d cap %d backing %p; upstream len %d cap %d; want original backing %p", len(got.Empty), cap(got.Empty), sliceBacking(got.Empty), len(want.Empty), cap(want.Empty), originalEmptyBacking)
			}
			if len(want.Empty) != 0 || cap(want.Empty) != 3 || sliceBacking(want.Empty) != baselineEmptyBacking {
				t.Fatalf("upstream absent empty slice baseline changed header: len=%d cap=%d backing=%p", len(want.Empty), cap(want.Empty), sliceBacking(want.Empty))
			}
		})
	}
}

func TestExplicitEmptySequenceReplacesPrefilledSliceHeader(t *testing.T) {
	t.Parallel()
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
			t.Parallel()
			const source = "empty: []\n"
			originalSlice := make([]*codecPlainStruct, 0, 3)
			originalBacking := sliceBacking(originalSlice)
			got := codecPlainContainers{Empty: originalSlice}

			baselineSlice := make([]*upstreamPlainStruct, 0, 3)
			want := upstreamPlainContainers{Empty: baselineSlice}
			if err := decodePrefilledSource(t, []byte(source), &got, selected); err != nil {
				t.Fatalf("Cursor.Decode: %v", err)
			}
			if err := upstream.Unmarshal([]byte(source), &want); err != nil {
				t.Fatalf("upstream.Unmarshal baseline: %v", err)
			}
			if got.Empty == nil || len(got.Empty) != 0 || cap(got.Empty) != cap(want.Empty) || cap(got.Empty) != 0 {
				t.Errorf("explicit empty sequence = %#v (len=%d cap=%d); upstream len=%d cap=%d; want an empty reset slice", got.Empty, len(got.Empty), cap(got.Empty), len(want.Empty), cap(want.Empty))
			}
			if cap(got.Empty) != 0 && sliceBacking(got.Empty) == originalBacking {
				t.Error("explicit empty sequence reused the old backing array")
			}
		})
	}
}

type codecPlainValue struct {
	Name    string `yaml:"name"`
	private string
}

type upstreamPlainValue struct {
	Name    string `yaml:"name"`
	private string
}

type codecValueMap struct {
	Values map[string]codecPlainValue `yaml:"values"`
	Other  string                     `yaml:"other"`
}

type upstreamValueMap struct {
	Values map[string]upstreamPlainValue `yaml:"values"`
	Other  string                        `yaml:"other"`
}

func TestPrefilledPlainValueMapKeepsPrivateStateWhenAbsent(t *testing.T) {
	t.Parallel()
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
			t.Parallel()
			const source = "other: decoded\n"
			got := codecValueMap{Values: map[string]codecPlainValue{"entry": {Name: "kept", private: "owned-private"}}}
			want := upstreamValueMap{Values: map[string]upstreamPlainValue{"entry": {Name: "kept", private: "owned-private"}}}
			if err := decodePrefilledSource(t, []byte(source), &got, selected); err != nil {
				t.Fatalf("Cursor.Decode: %v", err)
			}
			if err := upstream.Unmarshal([]byte(source), &want); err != nil {
				t.Fatalf("upstream.Unmarshal baseline: %v", err)
			}
			if !reflect.DeepEqual(got.Values["entry"], codecPlainValue{Name: want.Values["entry"].Name, private: want.Values["entry"].private}) {
				t.Errorf("prefilled value map entry = %#v; upstream name/private = %q/%q", got.Values["entry"], want.Values["entry"].Name, want.Values["entry"].private)
			}
		})
	}
}

type codecContainingValue struct {
	Name    string         `yaml:"name"`
	Child   prefilledChild `yaml:"child"`
	private string
}

type upstreamContainingValue struct {
	Name    string         `yaml:"name"`
	Child   prefilledChild `yaml:"child"`
	private string
}

type codecContainingPointer struct {
	Name    string         `yaml:"name"`
	Child   prefilledChild `yaml:"child"`
	private string
}

type upstreamContainingPointer struct {
	Name    string         `yaml:"name"`
	Child   prefilledChild `yaml:"child"`
	private string
}

type codecContainingContainers struct {
	Values   map[string]codecContainingValue    `yaml:"values"`
	Pointers map[string]*codecContainingPointer `yaml:"pointers"`
	Slice    []*codecContainingPointer          `yaml:"slice"`
}

type upstreamContainingContainers struct {
	Values   map[string]upstreamContainingValue    `yaml:"values"`
	Pointers map[string]*upstreamContainingPointer `yaml:"pointers"`
	Slice    []*upstreamContainingPointer          `yaml:"slice"`
}

func sameContainingValue(got codecContainingValue, want upstreamContainingValue) bool {
	return got.Name == want.Name && got.Child.Called == want.Child.Called && got.Child.Value == want.Child.Value && got.private == want.private
}

func sameContainingPointer(got *codecContainingPointer, want *upstreamContainingPointer) bool {
	return got != nil && want != nil && got.Name == want.Name && got.Child.Called == want.Child.Called && got.Child.Value == want.Child.Value && got.private == want.private
}

func TestContainingPlainStructsPreservePrefilledContainerState(t *testing.T) {
	t.Parallel()
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("Select=%t", selected), func(t *testing.T) {
			t.Parallel()
			const source = "values:\n  empty: {}\npointers:\n  empty: {}\n"
			keptValue := codecContainingValue{Name: "kept", Child: prefilledChild{Called: true, Value: "kept-child"}, private: "kept-private"}
			emptyValue := codecContainingValue{Name: "before", Child: prefilledChild{Called: true, Value: "before-child"}, private: "before-private"}
			keptPointer := &codecContainingPointer{Name: "kept", Child: prefilledChild{Called: true, Value: "kept-child"}, private: "kept-private"}
			emptyPointer := &codecContainingPointer{Name: "before", Child: prefilledChild{Called: true, Value: "before-child"}, private: "before-private"}
			sliceItem := &codecContainingPointer{Name: "slice", Child: prefilledChild{Called: true, Value: "slice-child"}, private: "slice-private"}
			slice := make([]*codecContainingPointer, 1, 3)
			slice[0] = sliceItem
			sliceStart := sliceBacking(slice)
			got := codecContainingContainers{
				Values:   map[string]codecContainingValue{"kept": keptValue, "empty": emptyValue},
				Pointers: map[string]*codecContainingPointer{"kept": keptPointer, "empty": emptyPointer},
				Slice:    slice,
			}

			baselineKeptValue := upstreamContainingValue{Name: "kept", Child: prefilledChild{Called: true, Value: "kept-child"}, private: "kept-private"}
			baselineEmptyValue := upstreamContainingValue{Name: "before", Child: prefilledChild{Called: true, Value: "before-child"}, private: "before-private"}
			baselineKeptPointer := &upstreamContainingPointer{Name: "kept", Child: prefilledChild{Called: true, Value: "kept-child"}, private: "kept-private"}
			baselineEmptyPointer := &upstreamContainingPointer{Name: "before", Child: prefilledChild{Called: true, Value: "before-child"}, private: "before-private"}
			baselineSliceItem := &upstreamContainingPointer{Name: "slice", Child: prefilledChild{Called: true, Value: "slice-child"}, private: "slice-private"}
			baselineSlice := make([]*upstreamContainingPointer, 1, 3)
			baselineSlice[0] = baselineSliceItem
			want := upstreamContainingContainers{
				Values:   map[string]upstreamContainingValue{"kept": baselineKeptValue, "empty": baselineEmptyValue},
				Pointers: map[string]*upstreamContainingPointer{"kept": baselineKeptPointer, "empty": baselineEmptyPointer},
				Slice:    baselineSlice,
			}

			if err := decodePrefilledSource(t, []byte(source), &got, selected); err != nil {
				t.Fatalf("Cursor.Decode: %v", err)
			}
			if err := upstream.Unmarshal([]byte(source), &want); err != nil {
				t.Fatalf("upstream.Unmarshal baseline: %v", err)
			}
			if want.Values["kept"] != baselineKeptValue || want.Values["kept"].private != "kept-private" {
				t.Fatalf("upstream absent value-map entry changed: %#v", want.Values["kept"])
			}
			if !sameContainingValue(got.Values["kept"], want.Values["kept"]) {
				t.Errorf("absent value-map entry = %#v, upstream = %#v", got.Values["kept"], want.Values["kept"])
			}
			if !sameContainingValue(got.Values["empty"], want.Values["empty"]) {
				t.Errorf("touched value-map entry `{}` = %#v, upstream = %#v", got.Values["empty"], want.Values["empty"])
			}
			if want.Pointers["kept"] != baselineKeptPointer || want.Pointers["kept"].private != "kept-private" {
				t.Fatalf("upstream absent pointer-map entry changed identity/state: %#v", want.Pointers["kept"])
			}
			if got.Pointers["kept"] != keptPointer || got.Pointers["kept"] == nil || got.Pointers["kept"].private != want.Pointers["kept"].private {
				t.Errorf("absent pointer-map entry = %#v (%p), upstream private state %q", got.Pointers["kept"], got.Pointers["kept"], want.Pointers["kept"].private)
			}
			if !sameContainingPointer(got.Pointers["empty"], want.Pointers["empty"]) {
				t.Errorf("touched pointer-map entry `{}` = %#v, upstream = %#v", got.Pointers["empty"], want.Pointers["empty"])
			}
			if (got.Pointers["empty"] == emptyPointer) != (want.Pointers["empty"] == baselineEmptyPointer) {
				t.Errorf("touched pointer-map entry identity differs from upstream: codec=%p (original %p), upstream=%p (original %p)", got.Pointers["empty"], emptyPointer, want.Pointers["empty"], baselineEmptyPointer)
			}
			if len(want.Slice) != 1 || want.Slice[0] != baselineSliceItem || sliceBacking(want.Slice) != sliceBacking(baselineSlice) {
				t.Fatalf("upstream absent slice changed identity/header: %#v", want.Slice)
			}
			if len(got.Slice) != 1 || got.Slice[0] != sliceItem || got.Slice[0].private != want.Slice[0].private || sliceBacking(got.Slice) != sliceStart {
				t.Errorf("absent slice = %#v cap=%d backing=%p; upstream private=%q cap=%d", got.Slice, cap(got.Slice), sliceBacking(got.Slice), want.Slice[0].private, cap(want.Slice))
			}
		})
	}
}

func decodePrefilledSource[T any](t *testing.T, data []byte, target *T, selected bool) error {
	t.Helper()
	if !selected {
		return decodeCursorSource(data, target)
	}
	data = append(append([]byte(nil), data...), []byte("selected: seen\n")...)
	root, err := codec.ParseYAML(data)
	if err != nil {
		return err
	}
	session, cursor, err := codec.Open(root)
	if err != nil {
		return err
	}
	defer session.Close()
	projection := struct {
		Payload  *T             `yaml:",inline"`
		Selected codec.Selected `yaml:"selected"`
	}{Payload: target}
	visits := 0
	err = cursor.Select(&projection, func(field *codec.Selected, occurrence codec.Cursor) error {
		visits++
		if field != &projection.Selected {
			t.Error("visitor received another field")
		}
		var value string
		if err := occurrence.Decode(&value); err != nil {
			return err
		}
		if value != "seen" {
			t.Errorf("selected value=%q", value)
		}
		return nil
	})
	if visits != 1 {
		t.Errorf("selected visits=%d want 1", visits)
	}
	return session.Finish(err)
}
