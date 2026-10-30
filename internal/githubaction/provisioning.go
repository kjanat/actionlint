package githubaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"actionlint.kjanat.dev/v2/internal/application"
	"actionlint.kjanat.dev/v2/internal/localapp"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/render"
)

func provisioningRequest(env func(string) string) (*lintRequest, error) {
	in, err := parseInputs(environmentArgs(env))
	if err != nil {
		return nil, err
	}
	a := &action{env: env}
	workspace, err := a.workspace()
	if err != nil {
		return nil, err
	}
	return a.prepareRequest(in, workspace)
}

func (req *lintRequest) pluginSelection() localapp.PluginSelection {
	return localapp.PluginSelection{ConfigFile: req.configFile, Overlays: req.overlays}.WithTools(req.disabledTools, req.disabledTools)
}

func (req *lintRequest) pluginInput() application.Input {
	if len(req.files) != 0 {
		return application.Input{Kind: application.Files, Paths: req.files}
	}
	return application.Input{Kind: application.Repository, Directory: req.workingDir}
}

// PluginPlan lets the launcher restore portable bytes before any plugin download.
func PluginPlan(env func(string) string, stdout, stderr io.Writer) int {
	req, err := provisioningRequest(env)
	if err == nil && req.configFile != "" {
		if _, readErr := os.Stat(req.configFile); readErr != nil {
			err = fmt.Errorf("could not read config file %q: %w", req.configFile, readErr)
		}
	}
	if err == nil {
		var selected localapp.PluginSources
		selected, err = req.pluginSelection().SelectForCache(context.Background(), req.workingDir, req.pluginInput())
		if err == nil {
			var plan localapp.PluginCachePlan
			plan, err = selected.CachePlan()
			if err == nil {
				err = json.NewEncoder(stdout).Encode(plan)
			}
		}
	}
	if err == nil {
		return render.ExitSuccessNoProblem
	}
	_, _ = fmt.Fprintln(stderr, err)
	if _, ok := errors.AsType[*inputError](err); ok {
		return render.ExitInvalidCommandOption
	}
	if _, ok := errors.AsType[*repoconfig.OverlayError](err); ok {
		return render.ExitInvalidCommandOption
	}
	return render.ExitFailure
}
