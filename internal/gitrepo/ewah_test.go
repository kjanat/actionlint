package gitrepo

import (
	"encoding/binary"
	"slices"
	"testing"
)

func bitmap(bitCount uint32, words ...uint64) []byte {
	out := binary.BigEndian.AppendUint32(nil, bitCount)
	out = binary.BigEndian.AppendUint32(out, uint32(len(words)))
	for _, word := range words {
		out = binary.BigEndian.AppendUint64(out, word)
	}
	return binary.BigEndian.AppendUint32(out, 0)
}

func marker(bit, runWords, literalWords uint64) uint64 {
	return bit | runWords<<1 | literalWords<<33
}

func readBitmap(t *testing.T, data []byte, limit int) []int {
	t.Helper()
	reader := indexReader{data: data}
	positions, err := reader.ewah(limit)
	if err != nil {
		t.Fatal(err)
	}
	if !reader.done() {
		t.Fatal("trailing data after the bitmap")
	}
	return positions
}

func TestEwah(t *testing.T) {
	if got := readBitmap(t, bitmap(0, 0), 0); len(got) != 0 {
		t.Fatalf("empty bitmap = %v", got)
	}
	if got := readBitmap(t, bitmap(0), 0); len(got) != 0 {
		t.Fatalf("wordless bitmap = %v", got)
	}
	if got := readBitmap(t, bitmap(4, marker(0, 0, 1), 0b1110), 4); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("literal word = %v", got)
	}
	if got := readBitmap(t, bitmap(70, marker(0, 0, 2), 1, 0b100000), 70); !slices.Equal(got, []int{0, 69}) {
		t.Fatalf("two literal words = %v", got)
	}
	want := make([]int, 0, 132)
	for i := 1; i < 131; i++ {
		want = append(want, i)
	}
	want = append(want, 132, 133)
	if got := readBitmap(t, bitmap(134, marker(0, 0, 1), ^uint64(1), marker(1, 1, 1), 0b110111), 134); !slices.Equal(got, want) {
		t.Fatalf("run of ones = %v", got)
	}
	if got := readBitmap(t, bitmap(130, marker(0, 2, 1), 1), 130); !slices.Equal(got, []int{128}) {
		t.Fatalf("run of zeros = %v", got)
	}
	if got := readBitmap(t, bitmap(2, marker(0, 0, 1), 0b1010), 2); !slices.Equal(got, []int{1}) {
		t.Fatalf("bits beyond the size = %v", got)
	}
	if got := readBitmap(t, bitmap(3, marker(1, 1, 0)), 3); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("run beyond the size = %v", got)
	}
	if got := readBitmap(t, bitmap(3, marker(1, 0xffff_ffff, 0)), 3); !slices.Equal(got, []int{0, 1, 2}) {
		t.Fatalf("huge run = %v", got)
	}
}

func TestEwahRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		data  []byte
		limit int
	}{
		"bit beyond the limit":  {bitmap(4, marker(0, 0, 1), 0b1000), 3},
		"run beyond the limit":  {bitmap(0xffff_ffff, marker(1, 0xffff_ffff, 0)), 10},
		"missing literal words": {bitmap(4, marker(0, 0, 2), 0b1110), 4},
		"too many words":        {append(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 4), 0xffff_ffff), 0, 0, 0, 0), 4},
	} {
		t.Run(name, func(t *testing.T) {
			reader := indexReader{data: tc.data}
			if got, err := reader.ewah(tc.limit); err == nil {
				t.Fatalf("ewah = %v, want an error", got)
			}
		})
	}
	full := bitmap(4, marker(0, 0, 1), 0b1110)
	for length := range full {
		reader := indexReader{data: full[:length]}
		if got, err := reader.ewah(4); err == nil {
			t.Fatalf("truncated to %d bytes: ewah = %v, want an error", length, got)
		}
	}
}
