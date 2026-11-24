package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"actionlint.kjanat.dev/v2/internal/base/files"
	"actionlint.kjanat.dev/v2/internal/model/toolmanifest"
)

// ConfigFile returns an absolute path, or "" when the search finds nothing.
// Native tools such as ShellCheck keep per-user files in the configuration directory on Windows.
func ConfigFile(reader *files.Reader, files toolmanifest.ConfigFiles, path, start string) (string, error) {
	if path != "" {
		return configFile(reader, files.Names, path)
	}
	for _, place := range files.Search {
		var found string
		var err error
		switch place {
		case toolmanifest.ConfigSearchUpward:
			found, err = searchUpward(files.Names, start)
		case toolmanifest.ConfigSearchHome:
			if home := homeDirectory(); home != "" {
				found, err = configInDirectory(files.Names, home)
			}
		case toolmanifest.ConfigSearchXDG:
			if directory := xdgConfigHome(); directory != "" {
				found, err = configInDirectory(files.Names, directory)
			}
		default:
			err = fmt.Errorf("unknown configuration search %q", place)
		}
		if err != nil {
			return "", err
		}
		if found != "" {
			return configFile(reader, files.Names, found)
		}
	}
	return "", nil
}

func searchUpward(names []string, start string) (string, error) {
	for directory := start; ; directory = filepath.Dir(directory) {
		found, err := configInDirectory(names, directory)
		if err != nil || found != "" {
			return found, err
		}
		if filepath.Dir(directory) == directory {
			return "", nil
		}
	}
}

func homeDirectory() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func xdgConfigHome() string {
	if runtime.GOOS == "windows" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		return directory
	}
	if directory := os.Getenv("XDG_CONFIG_HOME"); directory != "" {
		return directory
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

func configInDirectory(names []string, directory string) (string, error) {
	for _, name := range names {
		path := filepath.Join(directory, name)
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%q is not a regular configuration file", path)
		}
		return path, nil
	}
	return "", nil
}

func configFile(reader *files.Reader, names []string, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		found, err := configInDirectory(names, path)
		if err != nil {
			return "", err
		}
		if found == "" {
			return "", fmt.Errorf("directory %q contains none of %q", path, names)
		}
		path = found
	} else if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular configuration file", path)
	}
	if reader != nil {
		file, err := reader.Open(path)
		if err != nil {
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	return filepath.Abs(path)
}
