package actionlint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (rule *RuleShellcheck) sourcedDiagnostic(comment shellcheckError, directory string, sources map[string][]byte) *Error {
	path := comment.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}
	path = absPath(path)
	content, found := sources[path]
	if !found {
		var err error
		content, err = os.ReadFile(path)
		if err != nil {
			rule.Debug("Cannot read ShellCheck diagnostic source %q: %v", path, err)
			// Keep the diagnostic if the file disappeared, with an empty snippet.
			content = []byte{}
		}
		sources[path] = content
		if rule.onInput != nil {
			rule.onInput(path)
		}
	}
	finding := &Error{
		Filepath: path, Line: comment.Line, Column: comment.Column, Kind: rule.Name(),
		Message: fmt.Sprintf("shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", comment.Code, comment.Level, comment.Line, comment.Column, strings.TrimSuffix(comment.Message, ".")),
		source:  content, code: fmt.Sprintf("SC%d", comment.Code), severity: comment.Level,
	}
	if comment.EndLine >= comment.Line && comment.EndColumn > 0 {
		finding.endPosition = &Pos{Line: comment.EndLine, Col: comment.EndColumn}
		if comment.EndLine == comment.Line && comment.EndColumn > comment.Column {
			finding.endColumn = comment.EndColumn - 1
		}
	}
	return finding
}
