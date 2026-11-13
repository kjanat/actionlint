package repositoryid

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"
)

func known(t *testing.T, value uint64) ID {
	t.Helper()
	id, err := Known(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestKnownRejectsZero(t *testing.T) {
	t.Parallel()
	if _, err := Known(0); err == nil {
		t.Fatal("Known(0) returned no error")
	}
	value, ok := known(t, 42).Value()
	if value != 42 || !ok {
		t.Fatalf("Value() = %d, %t", value, ok)
	}
	if value, ok := (ID{}).Value(); value != 0 || ok {
		t.Fatalf("unknown Value() = %d, %t", value, ok)
	}
	if !(ID{}).IsZero() || known(t, 1).IsZero() {
		t.Fatal("IsZero does not match presence")
	}
}

func TestConflicts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		left, right ID
		want        bool
	}{
		{"both unknown", ID{}, ID{}, false},
		{"left unknown", ID{}, known(t, 1), false},
		{"right unknown", known(t, 1), ID{}, false},
		{"same", known(t, 7), known(t, 7), false},
		{"different", known(t, 7), known(t, 8), true},
	} {
		if got := test.left.Conflicts(test.right); got != test.want {
			t.Errorf("%s: Conflicts = %t, want %t", test.name, got, test.want)
		}
	}
}

type record struct {
	ID ID `json:"repository_id,omitzero"`
}

func TestJSON(t *testing.T) {
	t.Parallel()
	for name, marshal := range map[string]func(any) ([]byte, error){
		"v1": json.Marshal,
		"v2": func(v any) ([]byte, error) { return jsonv2.Marshal(v) },
	} {
		data, err := marshal(record{ID: known(t, 9007199254740999)})
		if err != nil || string(data) != `{"repository_id":9007199254740999}` {
			t.Fatalf("%s known: %s, %v", name, data, err)
		}
		data, err = marshal(record{})
		if err != nil || string(data) != `{}` {
			t.Fatalf("%s unknown: %s, %v", name, data, err)
		}
	}
	if _, err := json.Marshal(ID{}); err == nil {
		t.Fatal("marshalling an unknown ID returned no error")
	}
	var decoded record
	if err := json.Unmarshal([]byte(`{"repository_id":42}`), &decoded); err != nil || decoded.ID != known(t, 42) {
		t.Fatalf("decoded %+v, %v", decoded, err)
	}
	decoded = record{}
	if err := json.Unmarshal([]byte(`{}`), &decoded); err != nil || !decoded.ID.IsZero() {
		t.Fatalf("decoded absent %+v, %v", decoded, err)
	}
	for _, input := range []string{`0`, `-1`, `1.5`, `"1"`, `null`, `18446744073709551616`} {
		if err := json.Unmarshal([]byte(`{"repository_id":`+input+`}`), &decoded); err == nil {
			t.Errorf("decoding %s returned no error", input)
		}
	}
}
