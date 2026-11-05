package component

import (
	"math"
	"strconv"
	"testing"

	"github.com/kjanat/wacogo"
)

func TestRunnerPositionsAllowOnlyTheJobSentinelOrSourceCoordinates(t *testing.T) {
	for _, coordinate := range []struct{ line, column uint32 }{{0, 0}, {0, 1}, {1, 0}, {4, 14}, {math.MaxUint32, 1}, {1, math.MaxUint32}} {
		value := wacogo.NewValRecord(field("line", wacogo.ValU32(coordinate.line)), field("column", wacogo.ValU32(coordinate.column)))
		position, err := liftRunnerPosition(value)
		invalid := (coordinate.line == 0) != (coordinate.column == 0) || strconv.IntSize == 32 && (coordinate.line > math.MaxInt32 || coordinate.column > math.MaxInt32)
		if invalid {
			if err == nil {
				t.Fatalf("invalid runner coordinates accepted: %v", position)
			}
		} else if err != nil || uint64(position.Line) != uint64(coordinate.line) || uint64(position.Column) != uint64(coordinate.column) {
			t.Fatalf("runner coordinates changed: %+v err=%v", coordinate, err)
		}
		if coordinate.line == 0 && coordinate.column == 0 {
			if _, err := liftPosition(value); err == nil {
				t.Fatal("runner sentinel weakened diagnostic coordinates")
			}
		}
	}
	for _, value := range []wacogo.Val{
		wacogo.ValU32(0),
		wacogo.NewValRecord(field("line", wacogo.ValU32(0))),
		wacogo.NewValRecord(field("line", wacogo.ValU32(0)), field("column", wacogo.ValString("0"))),
	} {
		if _, err := liftRunnerPosition(value); err == nil {
			t.Fatal("malformed runner position accepted")
		}
	}
}
