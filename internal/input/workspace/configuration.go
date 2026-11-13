package workspace

import (
	"fmt"
	"sync"

	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	configreport "actionlint.kjanat.dev/v2/internal/input/configuration/report"
)

type configState struct {
	sync.Mutex
	source     *repoconfig.Loaded
	overlays   []repoconfig.Overlay
	onLoaded   func(configreport.Report)
	loaded     map[*Project]*repoconfig.Config
	provenance map[*Project]repoconfig.Provenance
	reports    map[*Project]configreport.Report
}

func (a *Session) ConfigForProject(project *Project) (*repoconfig.Config, error) {
	var cfg *repoconfig.Config
	var source *repoconfig.Loaded
	if a.defaultConfig != nil {
		cfg = a.defaultConfig
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
	var notification *configreport.Report
	defer func() {
		s.Unlock()
		if notification != nil && s.onLoaded != nil {
			s.onLoaded(*notification)
		}
	}()
	if loaded, ok := s.loaded[project]; ok {
		return loaded, nil
	}
	report := configreport.Report{Explicit: a.defaultConfig != nil}
	if project != nil {
		report.Project = project.RootDir()
	}
	if source != nil {
		report.File = source.Filename
	}
	for _, overlay := range s.overlays {
		report.Overrides = append(report.Overrides, overlay.Name())
	}
	cfg, resolved, err := a.rules.Overlaid(cfg, source, s.overlays, a.ruleNames...)
	if err != nil {
		return nil, err
	}
	resolved.Config = cfg
	resolved, err = a.networkPolicy.Resolve(a.rules, resolved)
	if err != nil {
		return nil, fmt.Errorf("configuration %q: %w", report.File, err)
	}
	cfg = resolved.Config
	report.Inspection = configreport.Inspection{Path: report.File, Config: resolved.Values, Origins: resolved.Origins, Warnings: resolved.Warnings}
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
	if s.provenance == nil {
		s.provenance = make(map[*Project]repoconfig.Provenance)
	}
	provenance := resolved.Provenance
	if source == nil && a.defaultConfig != nil {
		provenance = a.defaultConfigProvenance
	}
	s.provenance[project] = provenance
	s.reports[project] = configurationSnapshot(report)
	if s.onLoaded != nil {
		notify := configurationSnapshot(report)
		notification = &notify
	}
	return cfg, nil
}

// ConfigProvenanceForProject reads the source owner selected by ConfigForProject.
func (a *Session) ConfigProvenanceForProject(project *Project) repoconfig.Provenance {
	if a.configState == nil {
		if a.defaultConfig != nil {
			return a.defaultConfigProvenance
		}
		if project != nil && project.config != nil {
			return project.config.Provenance
		}
		return repoconfig.Provenance{}
	}
	a.configState.Lock()
	defer a.configState.Unlock()
	return a.configState.provenance[project]
}
