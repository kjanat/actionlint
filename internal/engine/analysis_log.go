package engine

import (
	"fmt"
	"io"
	"sync"
)

// LogLevel selects analysis logging verbosity.
type LogLevel int

const (
	// LogLevelNone does not output any log output.
	LogLevelNone LogLevel = 0
	// LogLevelVerbose shows verbose log output. This is equivalent to specifying -verbose option
	// to actionlint command.
	LogLevelVerbose = 1
	// LogLevelDebug shows all log output including debug information.
	LogLevelDebug = 2
)

type analysisLogger struct {
	logOut   io.Writer
	logLevel LogLevel
}

type analysisLogWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *analysisLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.out.Write(p)
}

func (l *analysisLogger) log(args ...any) {
	if l.logLevel >= LogLevelVerbose {
		_, _ = io.WriteString(l.logOut, "verbose: "+fmt.Sprintln(args...))
	}
}

func (l *analysisLogger) debug(format string, args ...any) {
	if l.logLevel >= LogLevelDebug {
		_, _ = fmt.Fprintf(l.logOut, "[Linter] "+format+"\n", args...)
	}
}

func (l *analysisLogger) debugWriter() io.Writer {
	if l.logLevel < LogLevelDebug {
		return nil
	}
	return l.logOut
}
