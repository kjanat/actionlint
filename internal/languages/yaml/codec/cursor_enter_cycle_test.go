package codec

import (
	"errors"
	"testing"

	upstream "github.com/kjanat/go-yaml/v4"
)

func TestCursorEnterCycleMatchesUpstreamAndRemainsFatal(t *testing.T) {
	const source = "&a {x: *a}\n"
	var baseline map[string]any
	wantErr := upstream.Unmarshal([]byte(source), &baseline)
	want, ok := errors.AsType[*upstream.LoadError](wantErr)
	if !ok || want.Stage != upstream.ComposerStage {
		t.Fatalf("upstream cycle error=%v", wantErr)
	}
	cursor := errorCursor(t, []byte(source))
	var first struct{ X Selected }
	visits := 0
	err := cursor.Select(&first, func(_ *Selected, occurrence Cursor) error {
		visits++
		entered, err := occurrence.Enter()
		if err != nil {
			return err
		}
		var second struct{ X Selected }
		return entered.Select(&second, func(_ *Selected, recursive Cursor) error {
			visits++
			_, err := recursive.Enter()
			return err
		})
	})
	fatal, ok := FatalLoadError(err)
	if !ok || visits != 2 || fatal.Message() != want.Message || fatal.Mark().Index != want.Mark.Index || fatal.Mark().Line != want.Mark.Line || fatal.Mark().Column != want.Mark.Column || fatal.Error() != want.Error() {
		t.Fatalf("entered cycle=%v visits=%d, upstream=%v", err, visits, want)
	}
	if cause, ok := errors.AsType[*upstream.LoadError](fatal); !ok || cause.Stage != want.Stage {
		t.Fatalf("cycle stage lost: %v", err)
	}
	value := "unchanged"
	_, enterErr := cursor.Enter()
	for _, next := range []error{cursor.Decode(&value), enterErr, cursor.session.Finish(nil)} {
		actual, ok := FatalLoadError(next)
		if !ok || actual != fatal {
			t.Fatalf("fatal cycle replaced: got=%v want=%v", next, fatal)
		}
	}
	if value != "unchanged" {
		t.Fatal("fatal cycle allowed later construction")
	}
}
