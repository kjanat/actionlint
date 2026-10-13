package gitrepo

import (
	"bufio"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// Git's object-file.h limits a loose object header to MAX_HEADER_LEN bytes.
const looseHeaderMaximum = 32

type inflater struct {
	source     *bufio.Reader
	stream     *bufio.Reader
	compressed uint64
}

func newInflater(compressed io.Reader, size uint64) (*inflater, error) {
	source := bufio.NewReader(compressed)
	stream, err := zlib.NewReader(source)
	if err != nil {
		return nil, err
	}
	return &inflater{source: source, stream: bufio.NewReader(stream), compressed: size}, nil
}

func (z *inflater) header() ([]byte, error) {
	var header []byte
	for len(header) < looseHeaderMaximum {
		c, err := z.stream.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("the header is unterminated")
		}
		if err != nil {
			return nil, err
		}
		if c == 0 {
			return header, nil
		}
		header = append(header, c)
	}
	return nil, fmt.Errorf("the header is longer than %d bytes", looseHeaderMaximum)
}

func (z *inflater) exact(size uint64) ([]byte, error) {
	if hi, limit := bits.Mul64(z.compressed, zlibMaximumRatio); hi == 0 && size > limit+64 {
		return nil, fmt.Errorf("%d compressed bytes claim %d bytes", z.compressed, size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(z.stream, data); err != nil {
		return nil, err
	}
	if n, err := z.stream.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("the stream does not end after %d bytes", size)
	}
	return data, nil
}

func (z *inflater) finished() error {
	if _, err := z.source.ReadByte(); !errors.Is(err, io.EOF) {
		return errors.New("garbage follows the compressed stream")
	}
	return nil
}
