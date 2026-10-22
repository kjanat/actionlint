package cli

import (
	"io"
	"sync"
)

type synchronizedLogWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *synchronizedLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(data)
}
