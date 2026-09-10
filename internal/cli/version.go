package cli

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"

	"actionlint.kjanat.dev"
)

type commandBuildInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	InstalledFrom string `json:"installed_from"`
	GoVersion     string `json:"go_version"`
	OS            string `json:"os"`
	GOARCH        string `json:"goarch"`
}

func commandBuild() commandBuildInfo {
	name := "actionlint"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
		name = info.Main.Path
	}
	return commandBuildInfo{name, actionlint.Version(), actionlint.InstalledFrom(), runtime.Version(), runtime.GOOS, runtime.GOARCH}
}

func writeVersion(out io.Writer, asJSON, legacy bool) error {
	b := commandBuild()
	if asJSON {
		return writeCommandJSON(out, b)
	}
	if !legacy {
		_, err := fmt.Fprintf(out, "actionlint %s\nInstalled: %s\nBuild: %s, %s/%s\n", b.Version, b.InstalledFrom, b.GoVersion, b.OS, b.GOARCH)
		return err
	}
	_, err := fmt.Fprintf(out, "%s %s\n%s\nbuilt with %s compiler for %s/%s\n", b.Name, b.Version, b.InstalledFrom, b.GoVersion, b.OS, b.GOARCH)
	return err
}
