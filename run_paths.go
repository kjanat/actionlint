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
	platform  platformKind
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

func (paths runPaths) effectiveRunDirectory(run *ExecRun, jobDir, workflowDir runDirectory) runDirectory {
	directory := runDirectory{directoryKnown, ""}
	for _, candidate := range []runDirectory{workingDirectoryValue(run.WorkingDirectory), jobDir, workflowDir} {
		if candidate.kind != directoryUnspecified {
			directory = candidate
			break
		}
	}
	if normalized, known := shellcheckDirectoryPath(directory.path, paths.platform); known {
		directory.path = normalized
	} else {
		directory.kind = directoryUnknown
	}
	return directory
}

func (paths runPaths) resolve(directory runDirectory) runDirectory {
	unknown := runDirectory{directoryUnknown, paths.analysis}
	if directory.kind == directoryUnknown {
		return unknown
	}
	local, ok := paths.analysisPath(directory.path)
	if !ok {
		return unknown
	}
	local, err := filepath.EvalSymlinks(local)
	if err != nil {
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
	relativePath, representable := runnerRelativePath(relativePath, paths.platform)
	if !representable {
		return "", false
	}
	path, known := paths.analysisPath(relativePath)
	if !known {
		return "", false
	}
	relative, err := filepath.Rel(paths.workspace, path)
	return path, err == nil && filepath.IsLocal(relative)
}

// Local analysis can follow available files outside the indexed repository.
func (paths runPaths) analysisPath(value string) (string, bool) {
	relativePath, known := shellcheckDirectoryPath(value, paths.platform)
	if !known {
		return "", false
	}
	if filepath.IsAbs(relativePath) {
		return relativePath, true
	}
	if strings.HasPrefix(relativePath, "/") || strings.ContainsRune(relativePath, '\x00') {
		return "", false
	}
	relativePath = filepath.FromSlash(relativePath)
	if paths.checkout != "" {
		var err error
		relativePath, err = filepath.Rel(filepath.FromSlash(paths.checkout), filepath.Clean(relativePath))
		if err != nil {
			return "", false
		}
	}
	return filepath.Join(paths.workspace, relativePath), true
}

func runnerRelativePath(value string, platform platformKind) (string, bool) {
	value, known := runnerDirectoryPath(value, platform)
	// Runner-absolute paths refer to the remote machine, not a local source root.
	return value, known && !filepath.IsAbs(value) && !strings.HasPrefix(value, "/") && !strings.ContainsRune(value, '\x00')
}
