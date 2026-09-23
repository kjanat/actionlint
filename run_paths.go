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
	workspace       string
	analysis        string
	checkout        string
	checkoutUnknown bool
	actionPath      string
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

func (paths runPaths) effectiveRunDirectory(run *ExecRun, jobDir, workflowDir runDirectory) runDirectory {
	directory := runDirectory{directoryKnown, ""}
	for _, candidate := range []runDirectory{paths.workingDirectory(run.WorkingDirectory), jobDir, workflowDir} {
		if candidate.kind != directoryUnspecified {
			directory = candidate
			break
		}
	}
	return directory
}

func (paths runPaths) workingDirectory(value *String) runDirectory {
	directory := workingDirectoryValue(value)
	if directory.kind != directoryUnknown {
		return directory
	}
	expression, ok := strings.CutPrefix(value.Value, "${{")
	if !ok {
		return directory
	}
	name, suffix, closed := strings.Cut(expression, "}}")
	if !closed || strings.Contains(suffix, "${{") || suffix != "" && !strings.HasPrefix(suffix, "/") {
		return directory
	}
	base := "."
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "github.workspace":
	case "github.action_path":
		if paths.actionPath == "" || paths.workspace == "" {
			return directory
		}
		relative, err := filepath.Rel(paths.workspace, paths.actionPath)
		if err != nil || !filepath.IsLocal(relative) {
			return directory
		}
		base = joinRunnerPath(paths.checkout, filepath.ToSlash(relative))
	default:
		return directory
	}
	// Keep runner paths relative until local() translates the self checkout.
	return runDirectory{directoryKnown, base + suffix}
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
	if paths.checkoutUnknown {
		return "", false
	}
	// Runner-absolute paths refer to the remote machine; they are not local source roots.
	windowsDrive := len(relativePath) >= 2 && relativePath[1] == ':' &&
		(relativePath[0] >= 'A' && relativePath[0] <= 'Z' || relativePath[0] >= 'a' && relativePath[0] <= 'z')
	if filepath.IsAbs(relativePath) || strings.HasPrefix(relativePath, "/") || windowsDrive {
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
