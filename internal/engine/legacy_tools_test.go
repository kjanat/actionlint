package engine

import "actionlint.kjanat.dev/internal/workspace"

type ExternalToolRequirements struct {
	Shellcheck bool `json:"shellcheck"`
	Pyflakes   bool `json:"pyflakes"`
	Ruff       bool `json:"ruff"`
}

func (a *AnalysisSession) RequiredTools(paths []string) (ExternalToolRequirements, error) {
	tools, err := a.Session.RequiredTools(a.ctx, paths, workspace.ToolOptions{Presets: a.request.RulePresets, Required: func(cfg, project *Config, inside bool) ([]string, error) {
		var names []string
		for _, r := range a.request.prepared().Tools {
			if r.NeedsProcess == nil {
				continue
			}
			needed, err := r.NeedsProcess(cfg, project, inside)
			if err != nil {
				return nil, err
			}
			if needed {
				names = append(names, r.Descriptor.Name)
			}
		}
		return names, nil
	}})
	return ExternalToolRequirements{Shellcheck: tools["shellcheck"], Ruff: tools["ruff"]}, err
}
