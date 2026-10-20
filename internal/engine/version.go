package engine

import (
	"runtime/debug"

	"actionlint.kjanat.dev/internal/render"
)

// These variables might be modified by ldflags on building release binaries by GoReleaser. Do not modify manually.
var (
	version       = ""
	installedFrom = ""
)

const (
	// ExitStatusSuccessNoProblem means linting completed without findings.
	ExitStatusSuccessNoProblem = render.ExitSuccessNoProblem
	// ExitStatusSuccessProblemFound means linting completed with findings.
	ExitStatusSuccessProblemFound = render.ExitSuccessProblemFound
	// ExitStatusInvalidCommandOption means command-line parsing or validation failed.
	ExitStatusInvalidCommandOption = render.ExitInvalidCommandOption
	// ExitStatusFailure means linting could not complete.
	ExitStatusFailure = render.ExitFailure
)

// InstalledFrom describes how the binary was installed.
func InstalledFrom() string {
	if installedFrom != "" {
		return installedFrom
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
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}
