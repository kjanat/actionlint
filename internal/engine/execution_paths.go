package engine

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Interpret runner path syntax before applying local filesystem operations.
func runnerDirectoryPath(path string, platform platformKind) (string, bool) {
	if strings.ContainsRune(path, ':') && (platform != platformKindMacOrLinux || runtime.GOOS == "windows") {
		return "", false
	}
	drivePrefix := len(path) >= 2 && path[1] == ':' &&
		(path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z')
	if drivePrefix {
		// On Unix, a:debug names a relative directory. Preserve that meaning for
		// downstream checks that reject remote Windows drive paths.
		path = "./" + path
	}
	if !strings.Contains(path, `\`) {
		return path, true
	}
	switch platform {
	case platformKindWindows:
		return strings.ReplaceAll(path, `\`, "/"), true
	case platformKindMacOrLinux:
		return path, runtime.GOOS != "windows"
	default:
		return "", false
	}
}

// Absolute paths can name available local directories when the path syntax
// agrees with the analysis host. Cross-platform runner paths remain unknown.
func localRunnerDirectoryPath(path string, platform platformKind) (string, bool) {
	if filepath.IsAbs(path) {
		compatible := platform == platformKindAny ||
			platform == platformKindWindows && runtime.GOOS == "windows" ||
			platform == platformKindMacOrLinux && runtime.GOOS != "windows"
		if !compatible {
			return "", false
		}
		return filepath.Clean(path), true
	}
	// Validate complete paths here; runnerDirectoryPath also normalizes
	// suffixes appended to resolved contexts, such as github.action_path.
	if platform == platformKindWindows && strings.HasPrefix(path, `\`) {
		return "", false
	}
	return runnerDirectoryPath(path, platform)
}
