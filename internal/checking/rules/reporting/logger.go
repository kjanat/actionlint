package reporting

import (
	"fmt"
	"io"
)

// Logger writes rule-prefixed debug messages to the supplied writer.
type Logger struct {
	Tag    string
	Writer io.Writer
}

// Debug writes a rule-prefixed message when a writer is configured.
func (r *Logger) Debug(format string, args ...any) {
	if r.Writer == nil {
		return
	}
	format = fmt.Sprintf("[%s] %s\n", r.Tag, format)
	_, _ = fmt.Fprintf(r.Writer, format, args...)
}

// EnableDebug selects the debug writer. Nil disables debug output.
func (r *Logger) EnableDebug(out io.Writer) {
	r.Writer = out
}
