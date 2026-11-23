package reporting

import (
	"fmt"
	"io"
)

type Logger struct {
	Tag    string
	Writer io.Writer
}

func (r *Logger) Debug(format string, args ...any) {
	if r.Writer == nil {
		return
	}
	format = fmt.Sprintf("[%s] %s\n", r.Tag, format)
	_, _ = fmt.Fprintf(r.Writer, format, args...)
}

// EnableDebug with a nil writer disables debug output.
func (r *Logger) EnableDebug(out io.Writer) {
	r.Writer = out
}
