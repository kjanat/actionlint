package githubaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"actionlint.kjanat.dev/v2/internal/checking"

	inputmodel "actionlint.kjanat.dev/v2/internal/input"
	"actionlint.kjanat.dev/v2/internal/input/workspace"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginacquisition"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"

	"actionlint.kjanat.dev/v2/internal/cli/exitcode"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
)

func provisioningRequest(env func(string) string, acquisition pluginacquisition.Settings) (*lintRequest, error) {
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

func (req *lintRequest) pluginSelection() pluginselection.Selection {
	defaults := req.requestedTools()
	defaults = slices.DeleteFunc(defaults, func(id string) bool { return req.processes[id] != nil })
	return pluginselection.WithTools(pluginselection.Selection{RuleSet: checking.ConfigurationRules(), ConfigFile: req.configFile, Overlays: req.overlays, Acquisition: req.acquisition, DefaultTools: defaults}, req.disabledTools, req.disabledTools)
}

func (req *lintRequest) pluginInput() inputmodel.Request {
	if len(req.files) != 0 {
		return inputmodel.Request{Kind: inputmodel.Files, Paths: req.files}
	}
	return inputmodel.Request{Kind: inputmodel.Repository, Directory: req.workingDir}
}

// PluginPlan lets the launcher restore portable bytes before any plugin download.
func PluginPlan(env func(string) string, stdout, stderr io.Writer, acquisition pluginacquisition.Settings) int {
	ctx := context.Background()
	req, err := provisioningRequest(env, acquisition)
	if err == nil && req.configFile != "" {
		if _, readErr := os.Stat(req.configFile); readErr != nil {
			err = fmt.Errorf("could not read config file %q: %w", req.configFile, readErr)
		}
	}
	if err == nil {
		var plan pluginselection.CachePlan
		plan, err = req.pluginPlan(ctx)
		if err == nil {
			err = json.NewEncoder(stdout).Encode(plan)
		}
	}
	if err == nil {
		return exitcode.ExitSuccessNoProblem
	}
	_, _ = fmt.Fprintln(stderr, err)
	if _, ok := errors.AsType[*inputError](err); ok {
		return exitcode.ExitInvalidCommandOption
	}
	if _, ok := errors.AsType[*configreport.OverlayError](err); ok {
		return exitcode.ExitInvalidCommandOption
	}
	return exitcode.ExitFailure
}

func (req *lintRequest) pluginPlan(ctx context.Context) (pluginselection.CachePlan, error) {
	inputs := []inputmodel.Request{req.pluginInput()}
	if len(req.files) != 0 {
		paths := slices.Clone(req.files)
		for i, path := range paths {
			if !filepath.IsAbs(path) {
				paths[i] = filepath.Join(req.workingDir, path)
			}
		}
		found, err := workspace.FindFiles(ctx, paths, false)
		if err != nil {
			return pluginselection.CachePlan{}, err
		}
		inputs = nil
		for _, batch := range workspace.PartitionFiles(found) {
			inputs = append(inputs, inputmodel.FromFiles(batch))
		}
	}
	var combined pluginselection.CachePlan
	if len(inputs) == 0 {
		inputs = []inputmodel.Request{{Kind: inputmodel.Stdin}}
	}
	for _, input := range inputs {
		selection := req.pluginSelection()
		selected, err := selection.SelectForCache(ctx, req.workingDir, input)
		if err != nil {
			return pluginselection.CachePlan{}, err
		}
		plan, err := selected.CachePlan(ctx)
		if err != nil {
			return pluginselection.CachePlan{}, err
		}
		combined.Directory = plan.Directory
		combined.Artifacts = append(combined.Artifacts, plan.Artifacts...)
	}
	slices.Sort(combined.Artifacts)
	combined.Artifacts = slices.Compact(combined.Artifacts)
	if combined.Artifacts == nil {
		combined.Artifacts = []string{}
	}
	return combined, nil
}
