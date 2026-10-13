package gitrepo

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func gitPrefix() string {
	exe, err := exec.LookPath("git")
	if err != nil {
		return "/usr"
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return "/usr"
	}
	prefix := filepath.Dir(filepath.Dir(exe))
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Base(prefix)) {
		case "mingw64", "mingw32", "clangarm64", "clang64", "ucrt64":
			prefix = filepath.Dir(prefix)
		}
	}
	return filepath.ToSlash(prefix)
}

func systemConfigPath() string {
	prefix := gitPrefix()
	if prefix == "/usr" {
		return "/etc/gitconfig"
	}
	return filepath.FromSlash(prefix + "/etc/gitconfig")
}
