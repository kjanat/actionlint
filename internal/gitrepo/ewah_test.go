package gitrepo

import (
	"encoding/binary"
	"errors"
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

var errBeyondLimit = errors.New("beyond the limit")

func readBitmap(t *testing.T, data []byte, limit uint64) ([]uint64, error) {
	t.Helper()
	reader := indexReader{data: data}
	words, err := reader.ewah()
	if err != nil {
		t.Fatal(err)
	}
	if !reader.done() {
		t.Fatal("trailing data after the bitmap")
	}
	var positions []uint64
	err = words.each(func(position uint64) error {
		if position >= limit {
			return errBeyondLimit
		}
		positions = append(positions, position)
		return nil
	})
	return positions, err
}

func TestEwah(t *testing.T) {
	ones := make([]uint64, 0, 132)
	for i := uint64(1); i < 131; i++ {
		ones = append(ones, i)
	}
	ones = append(ones, 132, 133)
	fullWord := make([]uint64, 64)
	for i := range fullWord {
		fullWord[i] = uint64(i)
	}
	for name, tc := range map[string]struct {
		data  []byte
		limit uint64
		want  []uint64
	}{
		"empty":                {bitmap(0, 0), 10, nil},
		"wordless":             {bitmap(0), 10, nil},
		"literal word":         {bitmap(4, marker(0, 0, 1), 0b1110), 10, []uint64{1, 2, 3}},
		"two literal words":    {bitmap(70, marker(0, 0, 2), 1, 0b100000), 70, []uint64{0, 69}},
		"run of ones":          {bitmap(134, marker(0, 0, 1), ^uint64(1), marker(1, 1, 1), 0b110111), 134, ones},
		"run of zeros":         {bitmap(130, marker(0, 2, 1), 1), 130, []uint64{128}},
		"bits beyond the size": {bitmap(2, marker(0, 0, 1), 0b1010), 10, []uint64{1, 3}},
		"run beyond the size":  {bitmap(3, marker(1, 1, 0)), 64, fullWord},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readBitmap(t, tc.data, tc.limit)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("positions = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestEwahStopsAtTheLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		data  []byte
		limit uint64
	}{
		"bit beyond the limit": {bitmap(4, marker(0, 0, 1), 0b1000), 3},
		"huge run of ones":     {bitmap(0xffff_ffff, marker(1, 0xffff_ffff, 0)), 10},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := readBitmap(t, tc.data, tc.limit); !errors.Is(err, errBeyondLimit) {
				t.Fatalf("positions = %v, %v, want the limit error", got, err)
			}
		})
	}
}

func TestEwahRejects(t *testing.T) {
	reader := indexReader{data: bitmap(4, marker(0, 0, 2), 0b1110)}
	words, err := reader.ewah()
	if err != nil {
		t.Fatal(err)
	}
	if err := words.each(func(uint64) error { return nil }); err == nil {
		t.Fatal("missing literal words were accepted")
	}
	tooMany := append(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 4), 0xffff_ffff), 0, 0, 0, 0)
	reader = indexReader{data: tooMany}
	if got, err := reader.ewah(); err == nil {
		t.Fatalf("ewah = %v, want an error for too many words", got)
	}
	full := bitmap(4, marker(0, 0, 1), 0b1110)
	for length := range full {
		reader := indexReader{data: full[:length]}
		if got, err := reader.ewah(); err == nil {
			t.Fatalf("truncated to %d bytes: ewah = %v, want an error", length, got)
		}
	}
}
