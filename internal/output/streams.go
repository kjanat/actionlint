package output

import (
	"encoding/json"
	"io"
)

type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func WriteJSONRecord(out io.Writer, value any) error {
	if writer, ok := out.(interface{ WriteJSON(any) error }); ok {
		return writer.WriteJSON(value)
	}
	return json.NewEncoder(out).Encode(value)
}
