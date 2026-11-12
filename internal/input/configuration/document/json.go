package document

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/tidwall/jsonc"
)

func parseJSON(path string, data []byte) (Document, error) {
	result := newDocument(path)
	clean := jsonc.ToJSON(data)
	if len(bytes.TrimSpace(clean)) == 0 {
		result.Value = map[string]any{}
		return result, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.UseNumber()
	contextualize := func(err error, offset int64) error {
		if syntax, ok := errors.AsType[*json.SyntaxError](err); ok {
			offset = syntax.Offset - 1
		}
		at := location(path, data, int(offset))
		return fmt.Errorf("%s:%d:%d: %w", path, at.Line, at.Column, err)
	}
	if err := decoder.Decode(&result.Value); err != nil {
		return result, contextualize(err, decoder.InputOffset())
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return result, contextualize(err, decoder.InputOffset())
		}
		return result, errors.New("must contain one document")
	}

	// Token offsets identify source locations and duplicate keys. The decoded
	// values above come from encoding/json and are not rebuilt by this walk.
	tokens := json.NewDecoder(bytes.NewReader(clean))
	tokens.UseNumber()
	start := func() int {
		offset := int(tokens.InputOffset())
		for offset < len(clean) && (clean[offset] == ' ' || clean[offset] == '\n' || clean[offset] == '\r' || clean[offset] == '\t' || clean[offset] == ':' || clean[offset] == ',') {
			offset++
		}
		return offset
	}
	var index func(string) error
	index = func(pointer string) error {
		at := start()
		token, err := tokens.Token()
		if err != nil {
			return err
		}
		result.ValueLocations[pointer] = location(path, data, at)
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for tokens.More() {
				at = start()
				token, err := tokens.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("expected object key")
				}
				if seen[key] {
					at := location(path, data, at)
					return fmt.Errorf("%s:%d:%d: duplicate key %q", path, at.Line, at.Column, key)
				}
				seen[key] = true
				child := Path(pointer, key)
				result.Locations[child] = location(path, data, at)
				if err := index(child); err != nil {
					return err
				}
			}
		case '[':
			for i := 0; tokens.More(); i++ {
				child := Index(pointer, i)
				result.Locations[child] = location(path, data, start())
				if err := index(child); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected delimiter %q", delimiter)
		}
		_, err = tokens.Token()
		return err
	}
	return result, index("")
}
