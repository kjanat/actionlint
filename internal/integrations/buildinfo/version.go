package buildinfo

import "runtime/debug"

// These variables might be modified by ldflags on building release binaries by GoReleaser. Do not modify manually.
var (
	ReleaseVersion = ""
	InstallOrigin  = ""
)

// InstalledFrom describes how the binary was installed.
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

// Version returns the release version or Go module build version.
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
