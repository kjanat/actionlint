package codec_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
	upstream "github.com/kjanat/go-yaml/v4"
)

type cursorPlainStrings struct {
	Items [][]string `yaml:"items"`
}

func walkCursorItems(root *syntax.Node, visit func(codec.Cursor) error) error {
	session, rootCursor, err := codec.Open(root)
	if err != nil {
		return err
	}
	defer session.Close()
	rootValue, err := rootCursor.Enter()
	if err != nil {
		return session.Finish(err)
	}
	found := false
	var walkErr error
	for key, value := range rootValue.Pairs() {
		name, err := key.Enter()
		if err != nil {
			walkErr = err
			break
		}
		if name.Node().Value != "items" {
			continue
		}
		found = true
		sequence, err := value.Enter()
		if err != nil {
			walkErr = err
			break
		}
		for item := range sequence.Items() {
			if err := visit(item); err != nil {
				walkErr = err
				break
			}
		}
		if walkErr != nil {
			break
		}
	}
	if !found && walkErr == nil {
		walkErr = errors.New("items mapping key not found")
	}
	return session.Finish(walkErr)
}

func cursorDecodeStrings(root *syntax.Node, enterItem bool) ([][]string, error) {
	var values [][]string
	err := walkCursorItems(root, func(item codec.Cursor) error {
		var value []string
		if enterItem {
			entered, err := item.Enter()
			if err != nil {
				return err
			}
			if err := entered.Decode(&value); err != nil {
				return err
			}
		} else {
			if err := item.Decode(&value); err != nil {
				return err
			}
		}
		values = append(values, value)
		return nil
	})
	return values, err
}

func cursorDecodeMaps(root *syntax.Node, enterItem bool) ([]map[string]any, error) {
	var values []map[string]any
	err := walkCursorItems(root, func(item codec.Cursor) error {
		var value map[string]any
		if enterItem {
			entered, err := item.Enter()
			if err != nil {
				return err
			}
			if err := entered.Decode(&value); err != nil {
				return err
			}
		} else {
			if err := item.Decode(&value); err != nil {
				return err
			}
		}
		values = append(values, value)
		return nil
	})
	return values, err
}

func assertCursorAliasErrorMatchesUpstream(t *testing.T, got, want error) {
	t.Helper()
	gotLoad, gotOK := errors.AsType[*codec.LoadError](got)
	wantLoad, wantOK := errors.AsType[*upstream.LoadError](want)
	if !gotOK || !wantOK {
		t.Fatalf("codec error = %T %v; upstream error = %T %v", got, got, want, want)
	}
	if gotLoad.Message != wantLoad.Message || gotLoad.Mark.Line != wantLoad.Mark.Line || gotLoad.Mark.Column != wantLoad.Mark.Column {
		t.Errorf("codec alias diagnostic (%q, %+v), upstream (%q, %+v)", gotLoad.Message, gotLoad.Mark, wantLoad.Message, wantLoad.Mark)
	}
	if gotLoad.Error() != wantLoad.Error() {
		t.Errorf("codec alias error text = %q, upstream = %q", gotLoad.Error(), wantLoad.Error())
	}
}

func assertOriginalItemAliases(t *testing.T, root *syntax.Node, anchorName string, count int) {
	t.Helper()
	anchor := mappingValue(root, anchorName)
	items := mappingValue(root, "items")
	if anchor == nil || items == nil || items.Kind != syntax.SequenceNode || len(items.Content) != count {
		t.Fatalf("source graph anchor=%#v items=%#v", anchor, items)
	}
	for i, occurrence := range items.Content {
		if occurrence.Kind != syntax.AliasNode || occurrence.Alias != anchor {
			t.Fatalf("source occurrence %d = %#v, want alias to original anchor %p", i, occurrence, anchor)
		}
	}
}

func TestCursorAliasBudgetMatchesUpstreamAndEnteredDecode(t *testing.T) {
	data := inheritedAliasSource()
	var baseline cursorPlainStrings
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want document-wide alias limit", wantErr)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse original alias graph: %v", err)
	}
	assertOriginalItemAliases(t, root, "payload", inheritedAliasOccurrences)
	for _, enterItem := range []bool{false, true} {
		got, gotErr := cursorDecodeStrings(root, enterItem)
		if gotErr == nil {
			t.Fatalf("cursor decode (Entered.Decode=%t) succeeded, want alias limit", enterItem)
		}
		assertCursorAliasErrorMatchesUpstream(t, gotErr, wantErr)
		if len(got) != 1 {
			t.Errorf("completed items before alias limit = %d, want one", len(got))
		}
	}
	assertOriginalItemAliases(t, root, "payload", inheritedAliasOccurrences)
}

func TestCursorStopsAtAliasLimitBeforeTrailingKeys(t *testing.T) {
	var source strings.Builder
	source.Write(inheritedAliasSource())
	for i := range 2500 {
		fmt.Fprintf(&source, "ignored%d: trailing\n", i)
	}
	data := []byte(source.String())
	var baseline cursorPlainStrings
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want alias limit before trailing fields", wantErr)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse source graph: %v", err)
	}
	for _, enterItem := range []bool{false, true} {
		_, gotErr := cursorDecodeStrings(root, enterItem)
		if gotErr == nil {
			t.Fatalf("cursor decode (Entered.Decode=%t) succeeded, want source-ordered alias limit", enterItem)
		}
		assertCursorAliasErrorMatchesUpstream(t, gotErr, wantErr)
	}
}

func TestCursorSplitAliasBudgetMatchesUpstream(t *testing.T) {
	data := splitAliasBudgetSource()
	var baseline upstreamAliasBudgetTarget
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want document-wide alias limit", wantErr)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse original split-alias graph: %v", err)
	}
	for _, enterItem := range []bool{false, true} {
		_, gotErr := cursorDecodeMaps(root, enterItem)
		if gotErr == nil {
			t.Fatalf("cursor decode (Entered.Decode=%t) succeeded, want alias limit", enterItem)
		}
		assertCursorAliasErrorMatchesUpstream(t, gotErr, wantErr)
	}
}

func TestCursorParentAliasSequenceCarriesExpansionContext(t *testing.T) {
	data := inheritedAliasSource()
	oldItems := "items:\n  - *payload\n  - *payload\n  - *payload\n"
	newItems := "sequence: &sequence\n  - *payload\n  - *payload\n  - *payload\nitems: *sequence\n"
	parentAlias := strings.Replace(string(data), oldItems, newItems, 1)
	if parentAlias == string(data) {
		t.Fatal("parent alias fixture replacement failed")
	}
	var baseline cursorPlainStrings
	wantErr := upstream.Unmarshal([]byte(parentAlias), &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream parent-alias baseline error = %v, want alias limit", wantErr)
	}
	root, err := codec.Parse([]byte(parentAlias))
	if err != nil {
		t.Fatalf("parse parent alias graph: %v", err)
	}
	sequence := mappingValue(root, "sequence")
	parentOccurrence := mappingValue(root, "items")
	payload := mappingValue(root, "payload")
	if sequence == nil || payload == nil || parentOccurrence == nil || parentOccurrence.Kind != syntax.AliasNode || parentOccurrence.Alias != sequence {
		t.Fatalf("source parent alias = %#v, sequence = %#v, payload = %#v", parentOccurrence, sequence, payload)
	}
	for i, occurrence := range sequence.Content {
		if occurrence.Kind != syntax.AliasNode || occurrence.Alias != payload {
			t.Fatalf("parent sequence item %d = %#v, want alias to original payload %p", i, occurrence, payload)
		}
	}
	for _, enterItem := range []bool{false, true} {
		_, gotErr := cursorDecodeStrings(root, enterItem)
		if gotErr == nil {
			t.Fatalf("parent alias cursor decode (Entered.Decode=%t) succeeded, want alias limit", enterItem)
		}
		assertCursorAliasErrorMatchesUpstream(t, gotErr, wantErr)
	}
	if parentOccurrence.Kind != syntax.AliasNode || parentOccurrence.Alias != sequence {
		t.Errorf("parent alias occurrence changed during decode: %#v", parentOccurrence)
	}
	for i, occurrence := range sequence.Content {
		if occurrence.Kind != syntax.AliasNode || occurrence.Alias != payload {
			t.Errorf("parent sequence item %d changed during decode: %#v", i, occurrence)
		}
	}
}

func TestCursorValidAliasAndUnselectedControlsMatchUpstream(t *testing.T) {
	literal := "[" + strings.TrimSuffix(strings.Repeat("value, ", inheritedAliasWidth), ", ") + "]"
	oneAlias := strings.Replace(string(inheritedAliasSource()), strings.Repeat("  - *payload\n", inheritedAliasOccurrences), "  - *payload\n", 1)
	literalItems := "items:\n" + strings.Repeat("  - "+literal+"\n", inheritedAliasOccurrences)
	ignoredBomb := strings.Replace(string(inheritedAliasSource()), "items:\n  - *payload\n  - *payload\n  - *payload\n", "ignored: &ignored [*payload, *payload, *payload]\nitems: [[ok]]\n", 1)
	for _, tc := range []struct {
		name   string
		source string
	}{{"one alias", oneAlias}, {"literal expansion", literalItems}, {"ignored alias field", ignoredBomb}} {
		t.Run(tc.name, func(t *testing.T) {
			var baseline cursorPlainStrings
			if err := upstream.Unmarshal([]byte(tc.source), &baseline); err != nil {
				t.Fatalf("upstream.Unmarshal: %v", err)
			}
			root, err := codec.Parse([]byte(tc.source))
			if err != nil {
				t.Fatalf("parse original graph: %v", err)
			}
			for _, enterItem := range []bool{false, true} {
				got, err := cursorDecodeStrings(root, enterItem)
				if err != nil {
					t.Fatalf("cursor decode (Entered.Decode=%t): %v", enterItem, err)
				}
				if !reflect.DeepEqual(got, baseline.Items) {
					t.Errorf("cursor result = %#v, upstream = %#v", got, baseline.Items)
				}
			}
		})
	}
}

func openFirstEnteredItem(root *syntax.Node) (*codec.Session, codec.Cursor, codec.Entered, error) {
	session, rootCursor, err := codec.Open(root)
	if err != nil {
		return nil, codec.Cursor{}, codec.Entered{}, err
	}
	rootValue, err := rootCursor.Enter()
	if err != nil {
		return session, codec.Cursor{}, codec.Entered{}, session.Finish(err)
	}
	for key, value := range rootValue.Pairs() {
		name, err := key.Enter()
		if err != nil {
			return session, codec.Cursor{}, codec.Entered{}, session.Finish(err)
		}
		if name.Node().Value != "items" {
			continue
		}
		items, err := value.Enter()
		if err != nil {
			return session, codec.Cursor{}, codec.Entered{}, session.Finish(err)
		}
		for item := range items.Items() {
			entered, err := item.Enter()
			if err != nil {
				return session, codec.Cursor{}, codec.Entered{}, session.Finish(err)
			}
			return session, item, entered, nil
		}
	}
	return session, codec.Cursor{}, codec.Entered{}, session.Finish(errors.New("items mapping key or item not found"))
}

func TestCursorZeroAndClosedSessionRejectOperations(t *testing.T) {
	assertInactive := func(err error) {
		t.Helper()
		if _, ok := errors.AsType[*codec.GraphError](err); !ok {
			t.Fatalf("inactive cursor error=%T %v, want GraphError", err, err)
		}
	}
	var zero codec.Cursor
	_, err := zero.Enter()
	assertInactive(err)
	var value []string
	assertInactive(zero.Decode(&value))
	var zeroEntered codec.Entered
	assertInactive(zeroEntered.Decode(&value))
	root, err := codec.Parse(inheritedAliasSource())
	if err != nil {
		t.Fatal(err)
	}
	session, item, entered, err := openFirstEnteredItem(root)
	if err != nil {
		t.Fatal(err)
	}
	session.Close()
	_, err = item.Enter()
	assertInactive(err)
	assertInactive(item.Decode(&value))
	assertInactive(entered.Decode(&value))
	var target struct {
		Value codec.Selected `yaml:"value"`
	}
	assertInactive(item.Select(&target, func(*codec.Selected, codec.Cursor) error { return nil }))
	assertInactive(entered.Select(&target, func(*codec.Selected, codec.Cursor) error { return nil }))
}

func TestCursorEnteredDecodeRepeatedCallCountsRootAgain(t *testing.T) {
	const width = 496
	var source strings.Builder
	source.WriteString("payload: &payload [")
	for i := range width {
		if i > 0 {
			source.WriteString(", ")
		}
		source.WriteString("value")
	}
	source.WriteString("]\npad: unused\nitems:\n  - *payload\n")
	data := []byte(source.String())
	var baseline cursorPlainStrings
	if err := upstream.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("upstream one-alias baseline: %v", err)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse source graph: %v", err)
	}
	session, _, entered, err := openFirstEnteredItem(root)
	if err != nil {
		t.Fatalf("enter alias occurrence: %v", err)
	}
	defer session.Close()
	var first []string
	if err := entered.Decode(&first); err != nil {
		t.Fatalf("first Entered.Decode: %v", err)
	}
	if !slices.Equal(first, baseline.Items[0]) {
		t.Fatalf("first decode has %d values, want %d", len(first), len(baseline.Items[0]))
	}
	enteredCopy := entered
	var second []string
	err = enteredCopy.Decode(&second)
	if err == nil || !strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("second Entered.Decode error = %v, want root counted again at alias limit", err)
	}
}

func TestCursorInvalidTargetDoesNotConsumeEnteredRoot(t *testing.T) {
	const width = 992
	var source strings.Builder
	source.WriteString("payload: &payload [")
	for i := range width {
		if i > 0 {
			source.WriteString(", ")
		}
		source.WriteString("value")
	}
	source.WriteString("]\npad: unused\nitems:\n  - *payload\n")
	data := []byte(source.String())
	var baseline cursorPlainStrings
	if err := upstream.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("upstream one-alias baseline: %v", err)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse source graph: %v", err)
	}
	session, _, entered, err := openFirstEnteredItem(root)
	if err != nil {
		t.Fatalf("enter alias occurrence: %v", err)
	}
	defer session.Close()
	var invalid *[]string
	if err := entered.Decode(invalid); err == nil || strings.Contains(err.Error(), "excessive aliasing") {
		t.Fatalf("invalid target error = %v, want target-validation error before counting", err)
	}
	var valid []string
	if err := entered.Decode(&valid); err != nil {
		t.Fatalf("valid decode after invalid target: %v", err)
	}
	if !slices.Equal(valid, baseline.Items[0]) {
		t.Errorf("valid decode has %d values, want %d", len(valid), len(baseline.Items[0]))
	}
}

func TestCursorFatalAliasBudgetIsSticky(t *testing.T) {
	data := inheritedAliasSource()
	var baseline cursorPlainStrings
	wantErr := upstream.Unmarshal(data, &baseline)
	if wantErr == nil || !strings.Contains(wantErr.Error(), "excessive aliasing") {
		t.Fatalf("upstream baseline error = %v, want alias limit", wantErr)
	}
	root, err := codec.Parse(data)
	if err != nil {
		t.Fatalf("parse cursor graph: %v", err)
	}
	var firstErr, secondErr, thirdErr error
	var third []string
	index := 0
	walkErr := walkCursorItems(root, func(item codec.Cursor) error {
		var value []string
		err := item.Decode(&value)
		switch index {
		case 0:
			firstErr = err
		case 1:
			secondErr = err
		case 2:
			third = []string{"untouched"}
			thirdErr = item.Decode(&third)
		}
		index++
		return nil
	})
	if firstErr != nil {
		t.Fatalf("first alias occurrence: %v", firstErr)
	}
	if secondErr == nil {
		t.Fatal("second alias occurrence succeeded, want document-wide limit")
	}
	assertCursorAliasErrorMatchesUpstream(t, secondErr, wantErr)
	if thirdErr == nil || thirdErr.Error() != secondErr.Error() {
		t.Fatalf("post-fatal error = %v, first fatal = %v", thirdErr, secondErr)
	}
	if !slices.Equal(third, []string{"untouched"}) {
		t.Errorf("post-fatal target changed: %#v", third)
	}
	if walkErr == nil || walkErr.Error() != secondErr.Error() {
		t.Errorf("Finish result = %v, want sticky fatal %v", walkErr, secondErr)
	}
}
