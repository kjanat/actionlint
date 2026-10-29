package execution

import (
	"path/filepath"
	"runtime"
	"strings"

	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// RunnerDirectoryPath interprets runner path syntax before local filesystem operations.
func RunnerDirectoryPath(path string, platform runnerlang.Platform) (string, bool) {
	if strings.ContainsRune(path, ':') && (platform != runnerlang.MacOrLinux || runtime.GOOS == "windows") {
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
	case runnerlang.Windows:
		return strings.ReplaceAll(path, `\`, "/"), true
	case runnerlang.MacOrLinux:
		return path, runtime.GOOS != "windows"
	case runnerlang.Any:
	}
	return "", false
}

// LocalDirectoryPath resolves runner paths whose syntax matches the analysis host.
func LocalDirectoryPath(path string, platform runnerlang.Platform) (string, bool) {
	if filepath.IsAbs(path) {
		compatible := platform == runnerlang.Any ||
			platform == runnerlang.Windows && runtime.GOOS == "windows" ||
			platform == runnerlang.MacOrLinux && runtime.GOOS != "windows"
		if !compatible {
			return "", false
		}
		return filepath.Clean(path), true
	}
	// Validate complete paths here; runnerDirectoryPath also normalizes
	// suffixes appended to resolved contexts, such as github.action_path.
	if platform == runnerlang.Windows && strings.HasPrefix(path, `\`) {
		return "", false
	}
	return RunnerDirectoryPath(path, platform)
}
