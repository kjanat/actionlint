package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"actionlint.kjanat.dev"
)

type commandResultWriter struct {
	io.Writer
	err error
}

func (w *commandResultWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

func resolveTemplate(r renderOptions) (renderOptions, error) {
	if r.TemplateFile != "" {
		data, err := os.ReadFile(r.TemplateFile)
		if err != nil {
			return r, fmt.Errorf("could not read template file %q: %w", r.TemplateFile, err)
		}
		r.Template = string(data)
		if r.Template == "" {
			return r, fmt.Errorf("template file %q is empty", r.TemplateFile)
		}
	}
	return r, nil
}

func writeCommandJSON(out io.Writer, value any) error {
	if terminal, ok := out.(*terminalJSONOutput); ok {
		return terminal.writeJSON(value)
	}
	return json.NewEncoder(out).Encode(value)
}

// Debug output can arrive in fragments and from concurrent file checks. Emit
// complete JSON log records without interleaving their contents.
type commandJSONLogWriter struct {
	mu      sync.Mutex
	pending string
	out     io.Writer
}

func (w *commandJSONLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending += string(p)
	for {
		line, rest, ok := strings.Cut(w.pending, "\n")
		if !ok {
			break
		}
		w.pending = rest
		if err := writeCommandJSON(w.out, struct {
			Log string `json:"log"`
		}{line}); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (a *commandApp) reportError(err error) int {
	status := actionlint.ExitStatusFailure
	if _, ok := errors.AsType[commandUsageError](err); ok {
		status = actionlint.ExitStatusInvalidCommandOption
	}
	if a.errorJSON || a.jsonOutput() {
		_ = writeCommandJSON(a.streams.Stderr, struct {
			Error    string `json:"error"`
			ExitCode int    `json:"exit_code"`
		}{err.Error(), status})
	} else {
		_, _ = fmt.Fprintln(a.streams.Stderr, err)
	}
	return status
}
