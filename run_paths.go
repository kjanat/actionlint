package actionlint

import (
	"os"
	"path/filepath"
	"strings"
)

type directoryKind uint8

const (
	directoryUnspecified directoryKind = iota
	directoryKnown
	directoryUnknown
)

type runDirectory struct {
	kind directoryKind
	path string
}

type runPaths struct {
	workspace string
	analysis  string
	checkout  string
}

func workingDirectoryValue(value *String) runDirectory {
	if value == nil {
		return runDirectory{}
	}
	if !value.ContainsExpression() {
		return runDirectory{directoryKnown, value.Value}
	}
	if literal, known := workflowExpressionLiteral(value); known {
		if text, ok := workflowScalarString(literal); ok {
			return runDirectory{directoryKnown, text}
		}
	}
	return runDirectory{kind: directoryUnknown}
}

func defaultsWorkingDirectory(defaults *Defaults) runDirectory {
	if defaults == nil || defaults.Run == nil {
		return runDirectory{}
	}
	if defaults.Run.Expression == nil {
		return workingDirectoryValue(defaults.Run.WorkingDirectory)
	}
	value, known := workflowExpressionLiteral(defaults.Run.Expression)
	if !known || len(workflowExpressionLiteralErrors(workflowDefaultsRun, value, "defaults.run")) != 0 {
		return runDirectory{kind: directoryUnknown}
	}
	if object, ok := value.(map[string]any); ok {
		for key, field := range object {
			if strings.EqualFold(key, "working-directory") {
				if text, ok := workflowScalarString(field); ok {
					return runDirectory{directoryKnown, text}
				}
				return runDirectory{kind: directoryUnknown}
			}
		}
	}
	return runDirectory{}
}

func effectiveRunDirectory(run *ExecRun, jobDir, workflowDir runDirectory) runDirectory {
	directory := runDirectory{directoryKnown, ""}
	for _, candidate := range []runDirectory{workingDirectoryValue(run.WorkingDirectory), jobDir, workflowDir} {
		if candidate.kind != directoryUnspecified {
			directory = candidate
			break
		}
	}
	return directory
}

func (paths runPaths) resolve(directory runDirectory) runDirectory {
	unknown := runDirectory{directoryUnknown, paths.analysis}
	if directory.kind == directoryUnknown {
		return unknown
	}
	local, ok := paths.local(directory.path)
	if !ok {
		return unknown
	}
	workspace, err := filepath.EvalSymlinks(paths.workspace)
	if err != nil {
		return unknown
	}
	local, err = filepath.EvalSymlinks(local)
	if err != nil {
		return unknown
	}
	relative, err := filepath.Rel(workspace, local)
	if err != nil || !filepath.IsLocal(relative) {
		return unknown
	}
	info, err := os.Stat(local)
	if err != nil || !info.IsDir() {
		return unknown
	}
	return runDirectory{directoryKnown, local}
}

// Translate a workspace-relative runner path to the local self checkout.
func (paths runPaths) local(relativePath string) (string, bool) {
	// Runner-absolute paths refer to the remote machine; they are not local source roots.
	if filepath.IsAbs(relativePath) || strings.HasPrefix(relativePath, "/") || strings.Contains(relativePath, ":") {
		return "", false
	}
	relativePath = filepath.FromSlash(relativePath)
	if paths.checkout != "" {
		var err error
		relativePath, err = filepath.Rel(filepath.FromSlash(paths.checkout), filepath.Clean(relativePath))
		if err != nil || !filepath.IsLocal(relativePath) {
			return "", false
		}
	}
	path := filepath.Join(paths.workspace, relativePath)
	relative, err := filepath.Rel(paths.workspace, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", false
	}
	return path, true
}
