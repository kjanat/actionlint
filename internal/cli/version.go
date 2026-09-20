package cli

import (
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"

	"actionlint.kjanat.dev"
)

var (
	releaseVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	// Go pseudo-versions and Makefile git-describe versions carry a commit suffix.
	developmentVersionPattern = regexp.MustCompile(`(?:[.-]\d{14}-|-\d+-g)([0-9a-f]+)(?:\+[0-9A-Za-z.-]+|-dirty)?$`)
)

func documentationRef(version string, info *debug.BuildInfo) string {
	development := developmentVersionPattern.FindStringSubmatch(version)
	if development == nil && !strings.HasSuffix(version, "-dirty") && releaseVersionPattern.MatchString(version) {
		return "v" + strings.TrimPrefix(version, "v")
	}
	if info != nil {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	if development != nil {
		return development[1]
	}
	// Builds without version or VCS metadata cannot identify their source commit.
	return "HEAD"
}

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
