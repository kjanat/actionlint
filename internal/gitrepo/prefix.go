package gitrepo

import (
	"os/exec"
	"path"
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
	return prefixOf(runtime.GOOS, filepath.ToSlash(exe))
}

func prefixOf(goos, exe string) string {
	prefix := path.Dir(path.Dir(exe))
	if goos == "windows" {
		switch strings.ToLower(path.Base(prefix)) {
		case "mingw64", "mingw32", "clangarm64", "clang64", "ucrt64":
			prefix = path.Dir(prefix)
		}
	}
	return prefix
}

func systemConfigPath() string {
	return systemConfigFor(gitPrefix())
}

func systemConfigFor(prefix string) string {
	if prefix == "/usr" || strings.HasPrefix(prefix, "/nix/store/") {
		return "/etc/gitconfig"
	}
	return filepath.FromSlash(prefix + "/etc/gitconfig")
}
