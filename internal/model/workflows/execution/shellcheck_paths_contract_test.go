package execution_test

import (
	"path/filepath"
	"runtime"
	"testing"

	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
)

func TestShellcheckAbsoluteDirectoryPlatform(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	for _, platform := range []runnerlang.Platform{runnerlang.Any, runnerlang.Windows, runnerlang.MacOrLinux} {
		path, known := execution.LocalDirectoryPath(directory, platform)
		wantKnown := platform == runnerlang.Any ||
			(platform == runnerlang.Windows) == (runtime.GOOS == "windows")
		if known != wantKnown || known && path != filepath.Clean(directory) {
			t.Fatalf("platform %v: got %q, known=%v; want known=%v", platform, path, known, wantKnown)
		}
	}
}

func TestShellcheckWindowsRootedDirectory(t *testing.T) {
	t.Parallel()
	for _, path := range []string{`\scripts\build`, `\\server\share`, `\\?\C:\scripts`} {
		if runtime.GOOS != "windows" {
			if got, known := execution.LocalDirectoryPath(path, runnerlang.Windows); known {
				t.Errorf("Windows path %q mapped to Unix path %q", path, got)
			}
		}
	}
	if got, known := execution.LocalDirectoryPath(`scripts\build`, runnerlang.Windows); !known || got != "scripts/build" {
		t.Errorf("relative Windows path lost: %q, known=%v", got, known)
	}
}
