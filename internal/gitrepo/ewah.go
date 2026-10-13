package gitrepo

import (
	"encoding/binary"
	"fmt"
	"math/bits"
)

func (r *indexReader) ewah(limit int) ([]int, error) {
	bitCount, err := r.uint32()
	if err != nil {
		return nil, err
	}
	wordCount, err := r.uint32()
	if err != nil {
		return nil, err
	}
	if wordCount > uint32(len(r.data)/8) {
		return nil, fmt.Errorf("the EWAH bitmap has %d words", wordCount)
	}
	raw, err := r.bytes(int(wordCount) * 8)
	if err != nil {
		return nil, err
	}
	if _, err := r.bytes(4); err != nil {
		return nil, err
	}
	words := make([]uint64, wordCount)
	for i := range words {
		words[i] = binary.BigEndian.Uint64(raw[i*8:])
	}
	var positions []int
	push := func(position uint64) error {
		if position >= uint64(limit) {
			return fmt.Errorf("the EWAH bitmap sets bit %d, beyond %d", position, limit)
		}
		positions = append(positions, int(position))
		return nil
	}
	offset := uint64(0)
	for index := 0; index < len(words); {
		marker := words[index]
		index++
		runWords := (marker >> 1) & 0xffff_ffff
		literalWords := marker >> 33
		runEnd := offset + runWords*64
		if runEnd < offset {
			runEnd = ^uint64(0)
		}
		if marker&1 == 1 {
			for position := offset; position < runEnd && position < uint64(bitCount); position++ {
				if err := push(position); err != nil {
					return nil, err
				}
			}
		}
		offset = runEnd
		if literalWords > uint64(len(words)-index) {
			return nil, fmt.Errorf("the EWAH bitmap has %d literal words but only %d words left", literalWords, len(words)-index)
		}
		for range literalWords {
			word := words[index]
			index++
			for word != 0 {
				position := offset + uint64(bits.TrailingZeros64(word))
				if position < uint64(bitCount) {
					if err := push(position); err != nil {
						return nil, err
					}
				}
				word &= word - 1
			}
			offset += 64
		}
	}
	return positions, nil
}
