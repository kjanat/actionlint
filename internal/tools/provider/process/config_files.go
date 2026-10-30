package process

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"actionlint.kjanat.dev/v2/internal/fsread"
	"actionlint.kjanat.dev/v2/internal/tools/provider"
)

// ConfigFile resolves a tool's native configuration file and records it with
// reader. A path naming a directory selects the first of files.Names in it.
// Without a path it searches the places files.Search lists, in order:
// "upward" walks from start to the filesystem root, "home" looks in the
// user's home directory and "xdg" in the XDG configuration directory (the
// user configuration directory on Windows). It returns an absolute path, or
// "" when the search finds nothing.
func ConfigFile(reader *fsread.Reader, files provider.ConfigFiles, path, start string) (string, error) {
	if path != "" {
		return configFile(reader, files.Names, path)
	}
	for _, place := range files.Search {
		var found string
		var err error
		switch place {
		case provider.ConfigSearchUpward:
			found, err = searchUpward(files.Names, start)
		case provider.ConfigSearchHome:
			var home string
			if home, err = os.UserHomeDir(); err == nil {
				found, err = configInDirectory(files.Names, home)
			} else {
				err = nil
			}
		case provider.ConfigSearchXDG:
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

// xdgConfigHome is the XDG configuration directory, or the user
// configuration directory on Windows; "" when neither is known.
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

// configInDirectory returns the first of names present in directory.
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

// configFile selects path, or the first of names inside a directory path,
// records it as read and returns it absolute.
func configFile(reader *fsread.Reader, names []string, path string) (string, error) {
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
	file, err := reader.Open(path)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return filepath.Abs(path)
}
