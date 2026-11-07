// Package transport reads and writes Content-Length-framed language-server messages.
package transport

import (
	"bufio"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func ReadMessage(r *bufio.Reader) (Message, error) {
	data, err := ReadPayload(r)
	if err != nil {
		return Message{}, err
	}
	var m Message
	err = jsonv2.Unmarshal(data, &m)
	return m, err
}

// ReadPayload reads one Content-Length-framed JSON-RPC payload. Browser hosts
// use it to carry unchanged server responses over a worker message channel.
func ReadPayload(r *bufio.Reader) (json.RawMessage, error) {
	length := -1
	headers := 0
	for {
		raw, err := r.ReadSlice('\n')
		line := string(raw)
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				return nil, errors.New("LSP header line exceeds limit")
			}
			return nil, err
		}
		headers += len(line)
		if headers > 8192 {
			return nil, errors.New("LSP headers exceed limit")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return nil, errors.New("duplicate Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 || length > 16<<20 {
		return nil, fmt.Errorf("invalid LSP message length %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

func WriteMessage(w io.Writer, value any) error {
	data, err := jsonv2.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
