// Package buildinfo identifies source references from release and Go build metadata.
package buildinfo

import (
	"regexp"
	"runtime/debug"
	"strings"
)

var (
	releaseVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	// Go pseudo-versions and Makefile git-describe versions carry a commit suffix.
	developmentVersionPattern = regexp.MustCompile(`(?:[.-]\d{14}-|-\d+-g)([0-9a-f]+)(?:\+[0-9A-Za-z.-]+|-dirty)?$`)
)

// SourceRef returns the matching release tag or source commit. Only builds
// without usable version or VCS metadata fall back to the development HEAD.
func SourceRef(version string, info *debug.BuildInfo) string {
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
	return "HEAD"
}
