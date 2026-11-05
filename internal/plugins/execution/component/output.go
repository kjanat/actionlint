package component

import (
	"bytes"
	"context"
	"fmt"
)

type hostImplementation struct {
	output *lineWriter
}

func (h hostImplementation) Log(_ context.Context, message string) error {
	// Preserve log-call boundaries without joining a partial stdio line.
	h.output.flush()
	_, err := h.output.Write([]byte(message))
	h.output.flush()
	return err
}

// lineWriter belongs to one invocation; its output budget is never reset.
type lineWriter struct {
	log     func(string)
	pending []byte
	written int
	err     error
}

const (
	maxPluginOutput = 1 << 20
	maxPluginLine   = 64 << 10
)

func (w *lineWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(data) > maxPluginOutput-w.written {
		w.err = fmt.Errorf("plugin output exceeds %d bytes per call", maxPluginOutput)
		return 0, w.err
	}
	w.written += len(data)
	n := len(data)
	for len(data) > 0 {
		count := min(len(data), maxPluginLine-len(w.pending))
		if index := bytes.IndexByte(data[:count], '\n'); index >= 0 {
			w.pending = append(w.pending, data[:index]...)
			w.flush()
			data = data[index+1:]
			continue
		}
		w.pending = append(w.pending, data[:count]...)
		data = data[count:]
		if len(w.pending) == maxPluginLine {
			w.flush()
		}
	}
	return n, nil
}

func (w *lineWriter) flush() {
	if len(w.pending) != 0 && w.log != nil {
		w.log(string(w.pending))
	}
	w.pending = nil
}
