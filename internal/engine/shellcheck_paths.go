package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"actionlint.kjanat.dev/internal/fsread"
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
func shellcheckDirectoryPath(path string, platform platformKind) (string, bool) {
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

func (run *shellcheckRun) prepareConfigPath() error {
	if run.paths.analysis == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		run.paths.analysis = cwd
	}
	if run.paths.workspace == "" {
		run.paths.workspace = run.paths.analysis
	}
	if err := run.prepareInlineConfig(); err != nil {
		return err
	}
	var selection ShellcheckConfigSelection
	base := run.pathContext().ConfigDir
	if run.config != nil && run.config.Config != nil {
		selection = run.config.Config
		base = "" // Application paths retain their process-working-directory origin.
	} else if config := run.cfg; config != nil {
		if path := config.Tools.Shellcheck.Config.Path(); path != "" {
			selection = ShellcheckRCFile(path)
			if config.Tools.Shellcheck.Config.FromInput() {
				base = run.paths.analysis
			}
		}
	}
	rcArgs := []string{"--norc"}
	var path string
	switch selected := selection.(type) {
	case nil:
		run.rcArgs = rcArgs
		return nil
	case ShellcheckRCMode:
		if selected == ShellcheckRCDisabled {
			run.rcArgs = rcArgs
			return nil
		}
		var err error
		path, err = discoverShellcheckRC(run.paths.reader, run.paths.analysis)
		if err != nil {
			return err
		}
		if path == "" {
			run.rcArgs = rcArgs
			return nil
		}
	case ShellcheckRCFile:
		var err error
		path, err = run.pathContext().ResolveFrom(string(selected), base)
		if err == nil {
			path, err = shellcheckRCFile(run.paths.reader, path)
		}
		if err != nil {
			return fmt.Errorf("tools.shellcheck.config: %w", err)
		}
	default:
		return errors.New("unsupported ShellCheck configuration selection")
	}
	run.rcArgs = []string{"--rcfile", path}
	return nil
}

func (run *shellcheckRun) prepareInlineConfig() error {
	run.inlineConfig = nil
	config := run.cfg
	if config == nil {
		return nil
	}
	inline := config.Tools.Shellcheck.Config.Inline()
	if inline == nil {
		return nil
	}
	resolved := *inline
	resolved.SourcePath = make([]string, len(inline.SourcePath))
	for i, path := range inline.SourcePath {
		var err error
		resolved.SourcePath[i], err = run.pathContext().Expand(path)
		if err != nil {
			return fmt.Errorf("tools.shellcheck.config.source-path: %w", err)
		}
	}
	run.inlineConfig = &resolved
	return nil
}

func shellcheckRCInDirectory(directory string) (string, error) {
	for _, name := range []string{".shellcheckrc", "shellcheckrc"} {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%q is not a regular ShellCheck configuration file", path)
		}
		return path, nil
	}
	return "", nil
}

func shellcheckRCFile(reader *fsread.Reader, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		found, err := shellcheckRCInDirectory(path)
		if err != nil {
			return "", err
		}
		if found == "" {
			return "", fmt.Errorf("directory %q contains neither .shellcheckrc nor shellcheckrc", path)
		}
		path = found
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular ShellCheck configuration file", path)
	}
	file, err := reader.Open(path)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return absPath(path), nil
}

func discoverShellcheckRC(reader *fsread.Reader, start string) (string, error) {
	for directory := start; ; directory = filepath.Dir(directory) {
		path, err := shellcheckRCInDirectory(directory)
		if err != nil {
			return "", err
		}
		if path != "" {
			return shellcheckRCFile(reader, path)
		}
		if filepath.Dir(directory) == directory {
			break
		}
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		if config, err := os.UserConfigDir(); err == nil {
			candidates = append(candidates, filepath.Join(config, "shellcheckrc"))
		}
	} else {
		config := os.Getenv("XDG_CONFIG_HOME")
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".shellcheckrc"))
			if config == "" {
				config = filepath.Join(home, ".config")
			}
		}
		if config != "" {
			candidates = append(candidates, filepath.Join(config, "shellcheckrc"))
		}
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return "", err
		}
		return shellcheckRCFile(reader, candidate)
	}
	return "", nil
}
