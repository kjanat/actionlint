package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

func (run *shellcheckRun) sourcedDiagnostic(comment shellcheckError, directory string, sources map[string][]byte) *Error {
	path := comment.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	path = absPath(path)
	content, found := sources[path]
	if !found {
		var err error
		content, err = run.paths.reader.ReadFile(path)
		if err != nil {
			run.Debug("Cannot read ShellCheck diagnostic source %q: %v", path, err)
			// Keep the diagnostic if the file disappeared, with an empty snippet.
			content = []byte{}
		}
		sources[path] = content
	}
	finding := &Error{
		Filepath: path, Line: comment.Line, Column: comment.Column, Kind: "shellcheck",
		Message: fmt.Sprintf("shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", comment.Code, comment.Level, comment.Line, comment.Column, strings.TrimSuffix(comment.Message, ".")),
		source:  content, rawScriptSource: true, code: fmt.Sprintf("SC%d", comment.Code), severity: comment.Level,
	}
	if comment.EndLine >= comment.Line && comment.EndColumn > 0 {
		finding.endPosition = &workflowlang.Pos{Line: comment.EndLine, Col: comment.EndColumn}
		if comment.EndLine == comment.Line && comment.EndColumn > comment.Column {
			finding.endColumn = comment.EndColumn - 1
		}
	}
	return finding
}
