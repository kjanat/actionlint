package codec_test

import (
	"reflect"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type ownedHookStruct struct {
	Name    string `yaml:"name"`
	private string
}

type ownedHookPayload ownedHookStruct

func (h *ownedHookStruct) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	return d.Decode(n, (*ownedHookPayload)(h))
}

type upstreamHookStruct struct {
	Name    string `yaml:"name"`
	private string
}

type upstreamHookPayload upstreamHookStruct

func (h *upstreamHookStruct) UnmarshalYAML(n *upstream.Node) error {
	return n.Decode((*upstreamHookPayload)(h))
}

type ownedHookContainers struct {
	Map   map[string]*ownedHookStruct `yaml:"map"`
	Slice []*ownedHookStruct          `yaml:"slice"`
	Empty []*ownedHookStruct          `yaml:"empty"`
	Other string                      `yaml:"other"`
}

type upstreamHookContainers struct {
	Map   map[string]*upstreamHookStruct `yaml:"map"`
	Slice []*upstreamHookStruct          `yaml:"slice"`
	Empty []*upstreamHookStruct          `yaml:"empty"`
	Other string                         `yaml:"other"`
}

func sliceBacking[T any](values []T) *T {
	if cap(values) == 0 {
		return nil
	}
	return &values[:cap(values)][0]
}

func TestPrefilledHookContainersKeepAbsentValuesAndIdentity(t *testing.T) {
	const source = "other: decoded\n"
	ownedEntry := &ownedHookStruct{Name: "kept", private: "owned-private"}
	ownedSliceEntry := &ownedHookStruct{Name: "slice-kept", private: "slice-private"}
	ownedSlice := make([]*ownedHookStruct, 1, 3)
	ownedSlice[0] = ownedSliceEntry
	ownedBacking := sliceBacking(ownedSlice)
	ownedEmpty := make([]*ownedHookStruct, 0, 3)
	ownedEmptyBacking := sliceBacking(ownedEmpty)
	got := ownedHookContainers{Map: map[string]*ownedHookStruct{"entry": ownedEntry}, Slice: ownedSlice, Empty: ownedEmpty, Other: "before"}

	baselineEntry := &upstreamHookStruct{Name: "kept", private: "owned-private"}
	baselineSliceEntry := &upstreamHookStruct{Name: "slice-kept", private: "slice-private"}
	baselineSlice := make([]*upstreamHookStruct, 1, 3)
	baselineSlice[0] = baselineSliceEntry
	baselineBacking := sliceBacking(baselineSlice)
	baselineEmpty := make([]*upstreamHookStruct, 0, 3)
	baselineEmptyBacking := sliceBacking(baselineEmpty)
	want := upstreamHookContainers{Map: map[string]*upstreamHookStruct{"entry": baselineEntry}, Slice: baselineSlice, Empty: baselineEmpty, Other: "before"}

	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Other != want.Other || got.Other != "decoded" {
		t.Errorf("decoded field = %q, upstream = %q; want decoded", got.Other, want.Other)
	}
	if want.Map["entry"] != baselineEntry || want.Map["entry"].private != ownedEntry.private {
		t.Fatalf("upstream map baseline changed entry identity/state: %#v", want.Map["entry"])
	}
	if entry := got.Map["entry"]; entry != ownedEntry || entry == nil || entry.private != want.Map["entry"].private {
		t.Errorf("absent map entry = %#v (%p), want original %#v (%p) and upstream private state %q", got.Map["entry"], got.Map["entry"], ownedEntry, ownedEntry, want.Map["entry"].private)
	}
	if len(got.Slice) != 1 || got.Slice[0] != ownedSliceEntry || got.Slice[0].private != ownedSliceEntry.private {
		t.Errorf("absent slice entry = %#v, want original pointer %#v", got.Slice, ownedSliceEntry)
	}
	if len(want.Slice) != 1 || want.Slice[0] != baselineSliceEntry || sliceBacking(want.Slice) != baselineBacking {
		t.Fatalf("upstream slice baseline changed entry/header identity: %#v", want.Slice)
	}
	if cap(got.Slice) != cap(want.Slice) || cap(got.Slice) != 3 || sliceBacking(got.Slice) != ownedBacking {
		t.Errorf("absent slice header = len %d cap %d backing %p; upstream len %d cap %d; want original backing %p", len(got.Slice), cap(got.Slice), sliceBacking(got.Slice), len(want.Slice), cap(want.Slice), ownedBacking)
	}
	if len(got.Empty) != 0 || cap(got.Empty) != cap(want.Empty) || cap(got.Empty) != 3 || sliceBacking(got.Empty) != ownedEmptyBacking {
		t.Errorf("absent empty slice header = len %d cap %d backing %p; upstream len %d cap %d; want original backing %p", len(got.Empty), cap(got.Empty), sliceBacking(got.Empty), len(want.Empty), cap(want.Empty), ownedEmptyBacking)
	}
	if len(want.Empty) != 0 || cap(want.Empty) != 3 || sliceBacking(want.Empty) != baselineEmptyBacking {
		t.Fatalf("upstream absent empty slice baseline changed header: len=%d cap=%d backing=%p", len(want.Empty), cap(want.Empty), sliceBacking(want.Empty))
	}
}

func TestExplicitEmptySequenceReplacesPrefilledSliceHeader(t *testing.T) {
	const source = "empty: []\n"
	ownedSlice := make([]*ownedHookStruct, 0, 3)
	ownedBacking := sliceBacking(ownedSlice)
	got := ownedHookContainers{Empty: ownedSlice}

	baselineSlice := make([]*upstreamHookStruct, 0, 3)
	want := upstreamHookContainers{Empty: baselineSlice}
	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if got.Empty == nil || len(got.Empty) != 0 || cap(got.Empty) != cap(want.Empty) || cap(got.Empty) != 0 {
		t.Errorf("explicit empty sequence = %#v (len=%d cap=%d); upstream len=%d cap=%d; want an empty reset slice", got.Empty, len(got.Empty), cap(got.Empty), len(want.Empty), cap(want.Empty))
	}
	if cap(got.Empty) != 0 && sliceBacking(got.Empty) == ownedBacking {
		t.Error("explicit empty sequence reused the old backing array")
	}
}

type ownedHookValue struct {
	Name    string `yaml:"name"`
	private string
}

type ownedHookValuePayload ownedHookValue

func (v *ownedHookValue) UnmarshalYAML(d *codec.Decoder, n *syntax.Node) error {
	return d.Decode(n, (*ownedHookValuePayload)(v))
}

type upstreamHookValue struct {
	Name    string `yaml:"name"`
	private string
}

type upstreamHookValuePayload upstreamHookValue

func (v *upstreamHookValue) UnmarshalYAML(n *upstream.Node) error {
	return n.Decode((*upstreamHookValuePayload)(v))
}

type ownedValueMap struct {
	Values map[string]ownedHookValue `yaml:"values"`
	Other  string                    `yaml:"other"`
}

type upstreamValueMap struct {
	Values map[string]upstreamHookValue `yaml:"values"`
	Other  string                       `yaml:"other"`
}

func TestPrefilledHookValueMapKeepsPrivateStateWhenAbsent(t *testing.T) {
	const source = "other: decoded\n"
	got := ownedValueMap{Values: map[string]ownedHookValue{"entry": {Name: "kept", private: "owned-private"}}}
	want := upstreamValueMap{Values: map[string]upstreamHookValue{"entry": {Name: "kept", private: "owned-private"}}}
	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
	}
	if err := upstream.Unmarshal([]byte(source), &want); err != nil {
		t.Fatalf("upstream.Unmarshal baseline: %v", err)
	}
	if !reflect.DeepEqual(got.Values["entry"], ownedHookValue{Name: want.Values["entry"].Name, private: want.Values["entry"].private}) {
		t.Errorf("prefilled value map entry = %#v; upstream name/private = %q/%q", got.Values["entry"], want.Values["entry"].Name, want.Values["entry"].private)
	}
}

type ownedContainingValue struct {
	Name    string        `yaml:"name"`
	Child   codecNodeHook `yaml:"child"`
	private string
}

type upstreamContainingValue struct {
	Name    string           `yaml:"name"`
	Child   upstreamNodeHook `yaml:"child"`
	private string
}

type ownedContainingPointer struct {
	Name    string        `yaml:"name"`
	Child   codecNodeHook `yaml:"child"`
	private string
}

type upstreamContainingPointer struct {
	Name    string           `yaml:"name"`
	Child   upstreamNodeHook `yaml:"child"`
	private string
}

type ownedContainingContainers struct {
	Values   map[string]ownedContainingValue    `yaml:"values"`
	Pointers map[string]*ownedContainingPointer `yaml:"pointers"`
	Slice    []*ownedContainingPointer          `yaml:"slice"`
}

type upstreamContainingContainers struct {
	Values   map[string]upstreamContainingValue    `yaml:"values"`
	Pointers map[string]*upstreamContainingPointer `yaml:"pointers"`
	Slice    []*upstreamContainingPointer          `yaml:"slice"`
}

func sameContainingValue(got ownedContainingValue, want upstreamContainingValue) bool {
	return got.Name == want.Name && got.Child.Called == want.Child.Called && got.Child.Value == want.Child.Value && got.private == want.private
}

func sameContainingPointer(got *ownedContainingPointer, want *upstreamContainingPointer) bool {
	return got != nil && want != nil && got.Name == want.Name && got.Child.Called == want.Child.Called && got.Child.Value == want.Child.Value && got.private == want.private
}

func TestContainingStructHooksPreservePrefilledContainerState(t *testing.T) {
	const source = "values:\n  empty: {}\npointers:\n  empty: {}\n"
	keptValue := ownedContainingValue{Name: "kept", Child: codecNodeHook{Called: true, Value: "kept-child"}, private: "kept-private"}
	emptyValue := ownedContainingValue{Name: "before", Child: codecNodeHook{Called: true, Value: "before-child"}, private: "before-private"}
	keptPointer := &ownedContainingPointer{Name: "kept", Child: codecNodeHook{Called: true, Value: "kept-child"}, private: "kept-private"}
	emptyPointer := &ownedContainingPointer{Name: "before", Child: codecNodeHook{Called: true, Value: "before-child"}, private: "before-private"}
	sliceItem := &ownedContainingPointer{Name: "slice", Child: codecNodeHook{Called: true, Value: "slice-child"}, private: "slice-private"}
	slice := make([]*ownedContainingPointer, 1, 3)
	slice[0] = sliceItem
	sliceStart := sliceBacking(slice)
	got := ownedContainingContainers{
		Values:   map[string]ownedContainingValue{"kept": keptValue, "empty": emptyValue},
		Pointers: map[string]*ownedContainingPointer{"kept": keptPointer, "empty": emptyPointer},
		Slice:    slice,
	}

	baselineKeptValue := upstreamContainingValue{Name: "kept", Child: upstreamNodeHook{Called: true, Value: "kept-child"}, private: "kept-private"}
	baselineEmptyValue := upstreamContainingValue{Name: "before", Child: upstreamNodeHook{Called: true, Value: "before-child"}, private: "before-private"}
	baselineKeptPointer := &upstreamContainingPointer{Name: "kept", Child: upstreamNodeHook{Called: true, Value: "kept-child"}, private: "kept-private"}
	baselineEmptyPointer := &upstreamContainingPointer{Name: "before", Child: upstreamNodeHook{Called: true, Value: "before-child"}, private: "before-private"}
	baselineSliceItem := &upstreamContainingPointer{Name: "slice", Child: upstreamNodeHook{Called: true, Value: "slice-child"}, private: "slice-private"}
	baselineSlice := make([]*upstreamContainingPointer, 1, 3)
	baselineSlice[0] = baselineSliceItem
	want := upstreamContainingContainers{
		Values:   map[string]upstreamContainingValue{"kept": baselineKeptValue, "empty": baselineEmptyValue},
		Pointers: map[string]*upstreamContainingPointer{"kept": baselineKeptPointer, "empty": baselineEmptyPointer},
		Slice:    baselineSlice,
	}

	if err := codec.Unmarshal([]byte(source), &got); err != nil {
		t.Fatalf("codec.Unmarshal: %v", err)
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
}
