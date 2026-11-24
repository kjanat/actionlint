package buildinfo

import "runtime/debug"

// GoReleaser sets these variables through ldflags.
var (
	ReleaseVersion = ""
	InstallOrigin  = ""
)

func InstalledFrom() string {
	if InstallOrigin != "" {
		return InstallOrigin
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		for _, s := range info.Settings {
			if s.Key == "vcs" {
				return "from source"
			}
		}
		return "go install"
	}
	return "from source"
}

func Version() string {
	if ReleaseVersion != "" {
		return ReleaseVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}
