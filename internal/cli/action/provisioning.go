package githubaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	plugininstall "actionlint.kjanat.dev/v2/internal/plugins/installation"

	commandstatus "actionlint.kjanat.dev/v2/internal/cli/exitcode"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
)

func provisioningRequest(env func(string) string, acquisition plugininstall.Acquisition) (*lintRequest, error) {
	in, err := parseInputs(environmentArgs(env))
	if err != nil {
		return nil, err
	}
	a := &action{env: env, acquisition: acquisition}
	workspace, err := a.workspace()
	if err != nil {
		return nil, err
	}
	return a.prepareRequest(in, workspace)
}

func (req *lintRequest) pluginSelection() plugininstall.Selection {
	defaults := req.requestedTools()
	defaults = slices.DeleteFunc(defaults, func(id string) bool { return req.processes[id] != nil })
	return plugininstall.WithTools(plugininstall.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: req.configFile, Overlays: req.overlays, Acquisition: req.acquisition, DefaultTools: defaults}, req.disabledTools, req.disabledTools)
}

func (req *lintRequest) pluginInput() inputmodel.Request {
	if len(req.files) != 0 {
		return inputmodel.Request{Kind: inputmodel.Files, Paths: req.files}
	}
	return inputmodel.Request{Kind: inputmodel.Repository, Directory: req.workingDir}
}

// PluginPlan lets the launcher restore portable bytes before any plugin download.
func PluginPlan(env func(string) string, stdout, stderr io.Writer, acquisition plugininstall.Acquisition) int {
	ctx := context.Background()
	req, err := provisioningRequest(env, acquisition)
	if err == nil && req.configFile != "" {
		if _, readErr := os.Stat(req.configFile); readErr != nil {
			err = fmt.Errorf("could not read config file %q: %w", req.configFile, readErr)
		}
	}
	if err == nil {
		var selected plugininstall.Sources
		selected, err = req.pluginSelection().SelectForCache(ctx, req.workingDir, req.pluginInput())
		if err == nil {
			var plan plugininstall.CachePlan
			plan, err = selected.CachePlan(ctx)
			if err == nil {
				err = json.NewEncoder(stdout).Encode(plan)
			}
		}
	}
	if err == nil {
		return commandstatus.ExitSuccessNoProblem
	}
	_, _ = fmt.Fprintln(stderr, err)
	if _, ok := errors.AsType[*inputError](err); ok {
		return commandstatus.ExitInvalidCommandOption
	}
	if _, ok := errors.AsType[*repoconfig.OverlayError](err); ok {
		return commandstatus.ExitInvalidCommandOption
	}
	return commandstatus.ExitFailure
}
