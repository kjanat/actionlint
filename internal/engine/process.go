package engine

import (
	"actionlint.kjanat.dev/internal/analyzerhost"
	"actionlint.kjanat.dev/internal/processconfig"
)

type concurrentProcess = analyzerhost.ProcessPool
type ExternalCommandOptions = processconfig.Options

func ResolveExternalCommandOptions(command string, options *ExternalCommandOptions) (string, []string, error) {
	return processconfig.ResolveOptions(command, options)
}
func ResolveExternalCommand(command string) (string, []string, error) {
	return processconfig.Resolve(command)
}
