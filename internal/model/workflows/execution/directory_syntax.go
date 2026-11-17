package execution

import (
	"path/filepath"
	"runtime"
	"strings"

	"actionlint.kjanat.dev/v2/internal/model/runners"
)

// RunnerDirectoryPath interprets runner path syntax before local filesystem operations.
func RunnerDirectoryPath(path string, platform runners.Platform) (string, bool) {
	if strings.ContainsRune(path, ':') && (platform != runners.MacOrLinux || runtime.GOOS == "windows") {
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
	case runners.Windows:
		return strings.ReplaceAll(path, `\`, "/"), true
	case runners.MacOrLinux:
		return path, runtime.GOOS != "windows"
	case runners.Any:
	}
	return "", false
}

// LocalDirectoryPath resolves runner paths whose syntax matches the analysis host.
func LocalDirectoryPath(path string, platform runners.Platform) (string, bool) {
	if filepath.IsAbs(path) {
		compatible := platform == runners.Any ||
			platform == runners.Windows && runtime.GOOS == "windows" ||
			platform == runners.MacOrLinux && runtime.GOOS != "windows"
		if !compatible {
			return "", false
		}
		return filepath.Clean(path), true
	}
	// Validate complete paths here; runnerDirectoryPath also normalizes
	// suffixes appended to resolved contexts, such as github.action_path.
	if platform == runners.Windows && strings.HasPrefix(path, `\`) {
		return "", false
	}
	return RunnerDirectoryPath(path, platform)
}
