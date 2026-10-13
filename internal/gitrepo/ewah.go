package gitrepo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
)

type ewahBitmap []uint64

func (r *indexReader) ewah() (ewahBitmap, error) {
	if _, err := r.uint32(); err != nil {
		return nil, errors.New("corrupt ewah bitmap: eof before bit size")
	}
	wordCount, err := r.uint32()
	if err != nil {
		return nil, errors.New("corrupt ewah bitmap: eof before length")
	}
	size := uint64(wordCount) * 8
	if size > uint64(len(r.data)-r.pos) {
		return nil, fmt.Errorf("corrupt ewah bitmap: eof in data (%d bytes short)", size-uint64(len(r.data)-r.pos))
	}
	raw := r.data[r.pos : r.pos+int(size)]
	r.pos += int(size)
	if _, err := r.bytes(4); err != nil {
		return nil, errors.New("corrupt ewah bitmap: eof before rlw")
	}
	words := make(ewahBitmap, wordCount)
	for i := range words {
		words[i] = binary.BigEndian.Uint64(raw[i*8:])
	}
	return words, nil
}

func (b ewahBitmap) each(visit func(position uint64) error) error {
	position := uint64(0)
	for index := 0; index < len(b); {
		marker := b[index]
		index++
		runWords := (marker >> 1) & 0xffff_ffff
		literalWords := marker >> 33
		if marker&1 == 1 {
			for range runWords * 64 {
				if err := visit(position); err != nil {
					return err
				}
				position++
			}
		} else {
			position += runWords * 64
		}
		if literalWords > uint64(len(b)-index) {
			return fmt.Errorf("the EWAH bitmap has %d literal words but only %d words left", literalWords, len(b)-index)
		}
		for range literalWords {
			for word := b[index]; word != 0; word &= word - 1 {
				if err := visit(position + uint64(bits.TrailingZeros64(word))); err != nil {
					return err
				}
			}
			index++
			position += 64
		}
	}
	return nil
}
