package codec_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "go.yaml.in/yaml/v4"
)

const aliasBudgetItems = 3
const aliasBudgetDepth = 3
const aliasBudgetWidth = 8

type upstreamAliasBudgetTarget struct {
	Items []map[string]any `yaml:"items"`
}

func splitAliasBudgetSource() []byte {
	var source strings.Builder
	source.WriteString("a0: &a0 value\n")
	for level := 1; level <= aliasBudgetDepth; level++ {
		fmt.Fprintf(&source, "a%d: &a%d [", level, level)
		for entry := range aliasBudgetWidth {
			if entry > 0 {
				source.WriteString(", ")
			}
			fmt.Fprintf(&source, "*a%d", level-1)
		}
		source.WriteString("]\n")
	}
	source.WriteString("items:\n")
	for range aliasBudgetItems {
		fmt.Fprintf(&source, "  - {value: *a%d}\n", aliasBudgetDepth)
	}
	return []byte(source.String())
}

func TestSelectedItemsShareAliasExpansionBudgetAndStopAfterFatal(t *testing.T) {
	data := splitAliasBudgetSource()
	var baseline upstreamAliasBudgetTarget
	wantErr := upstream.Unmarshal(data, &baseline)
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream error=%v want alias limit", wantErr)
	}
	var failures []error
	var beforeFatal int
	var firstFatal *codec.LoadError
	err = selectAliasItems(root, func(item codec.Cursor) error {
		value := map[string]any{"untouched": "keep"}
		decodeErr := item.Decode(&value)
		fatal, isFatal := codec.FatalLoadError(decodeErr)
		switch {
		case firstFatal != nil:
			if !isFatal || fatal != firstFatal {
				t.Errorf("later error=%p want identical fatal %p", decodeErr, firstFatal)
			}
			if !reflect.DeepEqual(value, map[string]any{"untouched": "keep"}) {
				t.Errorf("post-fatal target=%#v", value)
			}
		case decodeErr != nil:
			if !isFatal {
				t.Fatalf("unexpected nonfatal error: %v", decodeErr)
			}
			firstFatal = fatal
		default:
			beforeFatal++
		}
		failures = append(failures, decodeErr)
		return nil // Keep traversing so codec must enforce its sticky fatal state.
	})
	finished, isFatal := codec.FatalLoadError(err)
	if !isFatal || firstFatal == nil || finished != firstFatal {
		t.Fatalf("Finish=%v first fatal=%v", err, firstFatal)
	}
	assertCursorAliasErrorMatchesUpstream(t, err, wantErr)
	if len(failures) != aliasBudgetItems || beforeFatal != 0 {
		t.Fatalf("attempts=%d completed=%d failures=%v", len(failures), beforeFatal, failures)
	}
	last, isFatal := codec.FatalLoadError(failures[2])
	if !isFatal || last != firstFatal {
		t.Fatal("final attempt replaced fatal error")
	}
	items := mappingValue(root, "items")
	anchor := mappingValue(root, fmt.Sprintf("a%d", aliasBudgetDepth))
	if items == nil || anchor == nil || len(items.Content) != aliasBudgetItems {
		t.Fatal("source graph changed")
	}
	for _, item := range items.Content {
		alias := mappingValue(item, "value")
		if alias == nil || alias.Kind != syntax.AliasNode || alias.Alias != anchor {
			t.Errorf("source alias changed: %#v", alias)
		}
	}
}

const inheritedAliasWidth = 650
const inheritedAliasOccurrences = 3

type inheritedAliasTarget struct {
	Items [][]string `yaml:"items"`
}

func selectAliasItems(root *syntax.Node, visit func(codec.Cursor) error) error {
	session, cursor, err := codec.Open(root)
	if err != nil {
		return err
	}
	defer session.Close()
	var selected struct {
		Items codec.Selected `yaml:"items"`
	}
	err = cursor.Select(&selected, func(_ *codec.Selected, occurrence codec.Cursor) error {
		sequence, err := occurrence.Enter()
		if err != nil {
			return err
		}
		for item := range sequence.Items() {
			if err := visit(item); err != nil {
				return err
			}
		}
		return nil
	})
	return session.Finish(err)
}

func inheritedAliasSource() []byte {
	var source strings.Builder
	source.WriteString("payload: &payload [")
	for i := range inheritedAliasWidth {
		if i > 0 {
			source.WriteString(", ")
		}
		source.WriteString("value")
	}
	source.WriteString("]\nitems:\n")
	for range inheritedAliasOccurrences {
		source.WriteString("  - *payload\n")
	}
	return []byte(source.String())
}

func TestNestedHookKeepsAliasBudgetFromCapturedOccurrence(t *testing.T) {
	data := inheritedAliasSource()
	var baseline inheritedAliasTarget
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want document-wide alias limit", wantErr)
	}

	root, parseErr := codec.Parse(data)
	if parseErr != nil {
		t.Fatalf("parse owned graph: %v", parseErr)
	}
	items := mappingValue(root, "items")
	anchor := mappingValue(root, "payload")
	if items == nil || anchor == nil || len(items.Content) != inheritedAliasOccurrences {
		t.Fatalf("source graph has items=%#v anchor=%#v", items, anchor)
	}
	aliases := make([]*syntax.Node, 0, inheritedAliasOccurrences)
	for _, occurrence := range items.Content {
		if occurrence.Kind != syntax.AliasNode || occurrence.Alias != anchor {
			t.Fatalf("source occurrence = %#v, want original alias to payload %p", occurrence, anchor)
		}
		aliases = append(aliases, occurrence)
	}

	// Historical regression name retained for exact -run gates; implementation uses Select.
	var completed [][]string
	attempts := 0
	err := selectAliasItems(root, func(item codec.Cursor) error {
		var value []string
		err := item.Decode(&value)
		attempts++
		if err == nil {
			completed = append(completed, value)
		}
		return err
	})
	if len(completed) != 1 {
		t.Errorf("completed=%d want 1", len(completed))
	}
	if attempts != 2 {
		t.Errorf("attempted items=%d want 2", attempts)
	}
	if len(baseline.Items) != inheritedAliasOccurrences {
		t.Fatalf("upstream partial items=%d want %d", len(baseline.Items), inheritedAliasOccurrences)
	}
	if len(completed) == 1 && !reflect.DeepEqual(completed[0], baseline.Items[0]) {
		t.Error("completed item differs from upstream partial target")
	}
	for i, item := range baseline.Items[1:] {
		if item != nil {
			t.Errorf("upstream unfinished item %d=%v want nil", i+1, item)
		}
	}

	if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Errorf("selected codec error = %v, want upstream's alias limit inherited across captured aliases", err)
	}
	assertCursorAliasErrorMatchesUpstream(t, err, wantErr)
	for i, alias := range aliases {
		if alias.Kind != syntax.AliasNode || alias.Alias != anchor {
			t.Errorf("source alias occurrence %d changed during decode: %#v", i, alias)
		}
	}
}

func TestNestedHookAliasBudgetMatchesPlainDecode(t *testing.T) {
	aliased := string(inheritedAliasSource())
	oneAlias := strings.Replace(aliased, strings.Repeat("  - *payload\n", inheritedAliasOccurrences), "  - *payload\n", 1)
	literal := "[" + strings.TrimSuffix(strings.Repeat("value, ", inheritedAliasWidth), ", ") + "]"
	for _, tc := range []struct {
		name   string
		source string
		limit  bool
	}{
		{"one alias below limit", oneAlias, false},
		{"same values without expansion", "items:\n" + strings.Repeat("  - "+literal+"\n", inheritedAliasOccurrences), false},
		{"ignored alias expansion", strings.Replace(aliased, "items:", "ignored:", 1) + "items: [[value]]\n", false},
		{"aliased containing sequence", strings.Replace(aliased, "items:", "list: &list", 1) + "items: *list\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var plain inheritedAliasTarget
			plainErr := upstream.Unmarshal([]byte(tc.source), &plain)
			if tc.limit {
				if plainErr == nil || !strings.Contains(plainErr.Error(), "excessive aliasing") {
					t.Fatalf("upstream error = %v, want alias limit", plainErr)
				}
			} else if plainErr != nil {
				t.Fatalf("upstream rejected valid input: %v", plainErr)
			}

			root, parseErr := codec.Parse([]byte(tc.source))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			var got [][]string
			err := selectAliasItems(root, func(item codec.Cursor) error {
				var value []string
				err := item.Decode(&value)
				if err == nil {
					got = append(got, value)
				}
				return err
			})
			if tc.limit {
				if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
					t.Fatalf("codec error = %v, want alias limit", err)
				}
				assertCursorAliasErrorMatchesUpstream(t, err, plainErr)
				return
			}
			if err != nil {
				t.Fatalf("codec rejected valid input: %v", err)
			}
			if len(got) != len(plain.Items) {
				t.Fatalf("decoded %d items, want %d", len(got), len(plain.Items))
			}
			for i, item := range got {
				if !reflect.DeepEqual(item, plain.Items[i]) {
					t.Errorf("item %d differs from upstream", i)
				}
			}
		})
	}
}
