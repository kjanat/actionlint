package cli

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func renameNewReport(source, destination string) error {
	from, err := reportWindowsPath(source)
	if err != nil {
		return err
	}
	to, err := reportWindowsPath(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, 0)
}

func reportWindowsPath(name string) (*uint16, error) {
	path, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(path, `\\?\`), strings.HasPrefix(path, `\\.\`):
	case strings.HasPrefix(path, `\\`):
		path = `\\?\UNC\` + path[2:]
	default:
		path = `\\?\` + path
	}
	return windows.UTF16PtrFromString(path)
}
