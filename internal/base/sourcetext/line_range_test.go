package sourcetext

import "testing"

func TestLineRangeContainsAndOverlaps(t *testing.T) {
	t.Parallel()
	lines := LineRange{First: 2, Last: 4}
	for line, want := range map[int]bool{1: false, 2: true, 4: true, 5: false} {
		if got := lines.Contains(line); got != want {
			t.Errorf("Contains(%d) = %t, want %t", line, got, want)
		}
	}
	for _, tc := range []struct {
		other LineRange
		want  bool
	}{
		{LineRange{First: 1, Last: 1}, false},
		{LineRange{First: 1, Last: 2}, true},
		{LineRange{First: 3, Last: 3}, true},
		{LineRange{First: 4, Last: 9}, true},
		{LineRange{First: 5, Last: 9}, false},
	} {
		if got := lines.Overlaps(tc.other); got != tc.want {
			t.Errorf("Overlaps(%+v) = %t, want %t", tc.other, got, tc.want)
		}
	}
}
