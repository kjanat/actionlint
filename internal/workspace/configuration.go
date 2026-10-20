package workspace

import (
	"fmt"
	"sync"

	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

type configState struct {
	sync.Mutex
	source   *repoconfig.Loaded
	overlays []repoconfig.Overlay
	onLoaded func(repoconfig.Report)
	loaded   map[*Project]*repoconfig.Config
	reports  map[*Project]repoconfig.Report
}

func (a *Session) ConfigForProject(project *Project) (*repoconfig.Config, error) {
	var cfg *repoconfig.Config
	var source *repoconfig.Loaded
	if a.DefaultConfig != nil {
		cfg = a.DefaultConfig
		if a.configState != nil {
			source = a.configState.source
		}
	} else if project != nil {
		cfg, source = project.Config(), project.config
	}
	s := a.configState
	if s == nil {
		return cfg, nil
	}
	s.Lock()
	var notification *repoconfig.Report
	defer func() {
		s.Unlock()
		if notification != nil && s.onLoaded != nil {
			s.onLoaded(*notification)
		}
	}()
	if loaded, ok := s.loaded[project]; ok {
		return loaded, nil
	}
	report := repoconfig.Report{Explicit: a.DefaultConfig != nil}
	if project != nil {
		report.Project = project.RootDir()
	}
	if source != nil {
		report.File = source.Filename
	}
	for _, overlay := range s.overlays {
		report.Overrides = append(report.Overrides, overlay.Name())
	}
	cfg, resolved, err := repoconfig.Overlaid(cfg, source, s.overlays, a.ruleNames...)
	if err != nil {
		return nil, err
	}
	report.Inspection = repoconfig.Inspection{Path: report.File, Config: resolved.Values, Origins: resolved.Origins, Warnings: resolved.Warnings}
	if s.onLoaded == nil {
		for _, warning := range resolved.Warnings {
			file := warning.File
			if file == "" {
				file = report.File
			}
			_, _ = fmt.Fprintf(a.logOut, "%s:%d:%d: warning: %s\n", file, warning.Line, warning.Column, warning.Message)
		}
	}
	s.loaded[project] = cfg
	s.reports[project] = report
	notification = &report
	return cfg, nil
}
