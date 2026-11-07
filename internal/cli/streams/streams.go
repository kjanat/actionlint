// Package streams supplies command input and output without command parsing.
package streams

import (
	"encoding/json"
	"io"
)

// Streams supplies input, ordinary output and failure output for one command.
type Streams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// WriteJSON uses a writer's JSON presentation when supplied, or encodes one JSON record.
func WriteJSON(out io.Writer, value any) error {
	if writer, ok := out.(interface{ WriteJSON(any) error }); ok {
		return writer.WriteJSON(value)
	}
	return json.NewEncoder(out).Encode(value)
}
