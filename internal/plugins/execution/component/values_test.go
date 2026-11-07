package component

import (
	"math"
	"strconv"
	"testing"
)

func TestLowerU32Saturates(t *testing.T) {
	type testCase struct {
		input int
		want  uint64
	}
	cases := []testCase{{-1, 0}, {0, 0}, {1, 1}, {math.MaxInt32, math.MaxInt32}}
	if strconv.IntSize == 64 {
		limit := uint64(math.MaxUint32)
		cases = append(cases, testCase{int(limit), math.MaxUint32}, testCase{int(limit + 1), math.MaxUint32})
	}
	for _, tc := range cases {
		if got := uint64(lowerU32(tc.input)); got != tc.want {
			t.Errorf("lowerU32(%d)=%d, want %d on %d-bit int", tc.input, got, tc.want, strconv.IntSize)
		}
	}
}
