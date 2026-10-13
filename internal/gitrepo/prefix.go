package gitrepo

import (
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type installation struct {
	prefix       string
	systemConfig string
}

func gitInstallation() installation {
	exe, err := exec.LookPath("git")
	if err != nil {
		return installationOf(runtime.GOOS, "/usr/bin/git")
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return installationOf(runtime.GOOS, "/usr/bin/git")
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return installationOf(runtime.GOOS, filepath.ToSlash(exe))
}

func gitPrefix() string {
	return gitInstallation().prefix
}

func systemConfigPath() string {
	return gitInstallation().systemConfig
}

func installationOf(goos, exe string) installation {
	prefix := path.Dir(path.Dir(exe))
	if goos == "windows" {
		switch strings.ToLower(path.Base(prefix)) {
		case "mingw64", "mingw32", "clangarm64", "clang64", "ucrt64":
			prefix = path.Dir(prefix)
		}
	}
	if cellar := path.Dir(path.Dir(prefix)); path.Base(cellar) == "Cellar" && path.Base(path.Dir(prefix)) != "" {
		root := path.Dir(cellar)
		return installation{prefix: root + "/opt/" + path.Base(path.Dir(prefix)), systemConfig: filepath.FromSlash(root + "/etc/gitconfig")}
	}
	if prefix == "/usr" || (goos != "darwin" && strings.HasPrefix(prefix, "/nix/store/")) {
		return installation{prefix: prefix, systemConfig: filepath.FromSlash("/etc/gitconfig")}
	}
	return installation{prefix: prefix, systemConfig: filepath.FromSlash(prefix + "/etc/gitconfig")}
}
