package shellcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
)

func (run *shellcheckRun) recordSources(paths []string, directory string) error {
	for _, path := range paths {
		if path == "" || path == "-" || strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("invalid ShellCheck source path %q", path)
		}
		name := sourceName(path, directory)
		for end := 1; end <= len(name); end++ {
			if end != len(name) && !os.IsPathSeparator(name[end]) {
				continue
			}
			prefix := name[:end]
			info, err := os.Lstat(prefix)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			parent, base := filepath.Split(prefix)
			if resolved, err := filepath.EvalSymlinks(parent); err == nil {
				run.paths.reader.Record(filepath.Join(resolved, base))
			}
		}
		// The checker already read it. Keep the dependency even if it disappeared.
		run.paths.reader.Record(sourcePath(name, directory))
	}
	return nil
}

func sourceName(name, directory string) string {
	if !filepath.IsAbs(name) {
		name = directory + string(os.PathSeparator) + name
	}
	return name
}

func sourcePath(name, directory string) string {
	name = sourceName(name, directory)
	// Resolve symlinks before cleaning parent components.
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		return absPath(resolved)
	}
	return absPath(name)
}

func (run *shellcheckRun) sourcedDiagnostic(comment shellcheckError, directory string, sources map[string][]byte) *Error {
	path := sourcePath(comment.File, directory)
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
		Path: path, Start: DiagnosticPosition{Line: comment.Line, Column: comment.Column}, Rule: "shellcheck",
		Message: fmt.Sprintf("shellcheck reported issue in this script: SC%d:%s:%d:%d: %s", comment.Code, comment.Level, comment.Line, comment.Column, strings.TrimSuffix(comment.Message, ".")),
		source:  content, rawScriptSource: true, Code: fmt.Sprintf("SC%d", comment.Code), Severity: comment.Level,
	}
	if comment.EndLine >= comment.Line && comment.EndColumn > 0 {
		finding.endPosition = &workflowlang.Pos{Line: comment.EndLine, Col: comment.EndColumn}
		if comment.EndLine == comment.Line && comment.EndColumn > comment.Column {
			finding.endColumn = comment.EndColumn - 1
		}
	}
	return finding
}
