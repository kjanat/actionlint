package engine

import (
	"actionlint.kjanat.dev/internal/buildinfo"
	"actionlint.kjanat.dev/internal/render"
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

func InstalledFrom() string { return buildinfo.InstalledFrom() }
func Version() string       { return buildinfo.Version() }
