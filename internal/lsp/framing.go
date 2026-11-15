package lsp

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

// RPCMessage is one JSON-RPC request or notification.
type RPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// ReadRPCMessage reads and decodes one Content-Length-framed message.
func ReadRPCMessage(r *bufio.Reader) (RPCMessage, error) {
	data, err := ReadFramedPayload(r)
	if err != nil {
		return RPCMessage{}, err
	}
	var m RPCMessage
	err = jsonv2.Unmarshal(data, &m)
	return m, err
}

// ReadFramedPayload reads one Content-Length-framed JSON-RPC payload. Browser hosts
// use it to carry unchanged server responses over a worker message channel.
func ReadFramedPayload(r *bufio.Reader) (json.RawMessage, error) {
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

// WriteRPCMessage writes value as one Content-Length-framed JSON message.
func WriteRPCMessage(w io.Writer, value any) error {
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
