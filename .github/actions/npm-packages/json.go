package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// object is a JSON object that remembers the order its keys were written in.
//
// A published package.json is read by people as well as by npm, so the key
// order of the checked-in template is worth preserving. Decoding into a map and
// re-encoding sorts the keys alphabetically and scrambles the manifest.
type object struct {
	keys   []string
	values map[string]json.RawMessage
}

// parseObject decodes a JSON object, recording the order of its keys.
func parseObject(data []byte) (*object, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object, got %v", tok)
	}

	o := &object{values: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected an object key, got %v", tok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decoding %q: %w", key, err)
		}
		if _, seen := o.values[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.values[key] = raw
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return nil, err
	}
	return o, nil
}

// set replaces a key's value, keeping its position, or appends it when new.
func (o *object) set(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %q: %w", key, err)
	}
	if _, seen := o.values[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.values[key] = raw
	return nil
}

// has reports whether the object carries a key.
func (o *object) has(key string) bool {
	_, ok := o.values[key]
	return ok
}

// encode renders the object with two-space indentation and a trailing newline,
// matching what npm and dprint produce for a package.json.
func (o *object) encode() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range o.keys {
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, o.values[key], "  ", "  "); err != nil {
			return nil, fmt.Errorf("indenting %q: %w", key, err)
		}
		fmt.Fprintf(&buf, "  %s: %s", name, pretty.String())
		if i < len(o.keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
