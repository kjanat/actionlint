package codec

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

type admissionLimits struct {
	counts [][2]int
	stopAt int
}

func (*admissionLimits) CheckDepth(int, *upstream.DepthContext) error { return nil }

func (limits *admissionLimits) CheckAlias(aliases, constructions int) error {
	limits.counts = append(limits.counts, [2]int{aliases, constructions})
	if constructions == limits.stopAt {
		return errors.New("test expansion limit")
	}
	return nil
}

func TestAdmissionCountsSharedOccurrencesAndStopsAtRejection(t *testing.T) {
	root, err := Parse([]byte("a: &a [x]\nb: *a\nc: *a\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stopAt := range []int{0, 9} {
		session, cursor, err := Open(root)
		if err != nil {
			t.Fatal(err)
		}
		limits := &admissionLimits{stopAt: stopAt}
		session.limits = limits
		err = session.Finish(admit(cursor, func(*syntax.Node, int) bool { return true }))
		session.Close()
		want := [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}, {0, 6}, {0, 7}, {1, 8}, {2, 9}, {2, 10}, {2, 11}, {3, 12}, {4, 13}}
		if stopAt != 0 {
			want = want[:stopAt]
			failure, ok := errors.AsType[*LoadError](err)
			leaf := root.Content[0].Content[1].Content[0]
			if !ok || failure.Message != "test expansion limit" || failure.Mark.Line != leaf.Line || failure.Mark.Column != leaf.Column {
				t.Fatalf("rejection lost physical mark: %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(limits.counts, want) {
			t.Fatalf("counts=%v, want %v", limits.counts, want)
		}
	}
}

func TestAdmissionSelectionUsesParentSlots(t *testing.T) {
	root, err := Parse([]byte("a: &a [x]\nb: *a\nc: *a\n"))
	if err != nil {
		t.Fatal(err)
	}
	mapping := root.Content[0]
	// One physical alias can occupy distinct slots in a hand-built graph.
	mapping.Content[5] = mapping.Content[3]
	session, cursor, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	limits := &admissionLimits{}
	session.limits = limits
	err = admit(cursor, func(parent *syntax.Node, index int) bool { return parent == mapping && index == 5 })
	if err != nil {
		t.Fatal(err)
	}
	if got := limits.counts[len(limits.counts)-1]; got != [2]int{2, 10} {
		t.Fatalf("rejected alias slot counted or accepted slot deduplicated: %v", limits.counts)
	}
}

func TestAdmissionOnlyFollowsSelectedRecursiveAliases(t *testing.T) {
	root, err := Parse([]byte("a: &a [*a]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Admit(root, func(*syntax.Node, int) bool { return false }); err != nil {
		t.Fatal(err)
	}
	err = Admit(root, func(*syntax.Node, int) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "contains itself") {
		t.Fatalf("accepted recursion must fail: %v", err)
	}
	if _, ok := errors.AsType[*AdmissionError](err); !ok {
		t.Fatal("admission rejection lost its typed boundary")
	}
	if _, ok := errors.AsType[*LoadError](err); !ok {
		t.Fatal("admission rejection lost its load cause")
	}
}

func TestAdmissionFailuresAlwaysCarryRejectionType(t *testing.T) {
	root, err := Parse([]byte("value: text\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{
		Admit(root, nil),
		Admit(nil, func(*syntax.Node, int) bool { return true }),
	} {
		if _, ok := errors.AsType[*AdmissionError](failure); !ok {
			t.Fatalf("failed admission lacked rejection type: %v", failure)
		}
	}
}

func TestAdmissionShortcutMatchesOwnedPolicy(t *testing.T) {
	root, err := Parse([]byte("value: text\n"))
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if RequiresAdmission(0) || !RequiresAdmission(1) {
		t.Fatal("admission shortcut does not distinguish accepted crossings")
	}
	for constructions := 1; constructions <= math.MaxInt/2; constructions *= 2 {
		if err := session.limits.CheckAlias(0, constructions); err != nil {
			t.Fatal("alias-free shortcut no longer matches the resource policy", err)
		}
	}
	if err := session.limits.CheckAlias(0, math.MaxInt); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionErrorZeroValuesAndFatalPrecedence(t *testing.T) {
	for _, rejected := range []*AdmissionError{nil, {}} {
		if rejected.Error() != "YAML admission rejected" || rejected.Unwrap() != nil {
			t.Fatal("zero-valued admission rejection is unsafe")
		}
	}
	schema := &DecodeError{Errors: []*LoadError{{Message: "schema"}}}
	resource := &LoadError{Message: "resource", Mark: Mark{Line: 3, Column: 4}}
	joined := errors.Join(schema, NewAdmissionError(resource))
	if got, fatal := FatalLoadError(joined); !fatal || got != resource {
		t.Fatal("schema errors hid a joined admission rejection", got)
	}
}
