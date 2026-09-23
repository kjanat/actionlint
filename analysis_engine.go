package actionlint

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"
)

type analysisEngine struct {
	analysisLogger
	ctx                                context.Context
	shellcheck, pyflakes               string
	shellcheckOptions, pyflakesOptions *ExternalCommandOptions
	shellcheckSettings                 *ShellcheckSettings
	workingDir                         string
	inputs                             *inputFiles
	gitModes                           *gitModes
	ignorePats                         IgnorePatterns
	onRulesCreated                     func([]Rule) []Rule
}

func (l *analysisEngine) check(
	path string,
	content []byte,
	project *Project,
	cfg *Config,
	proc *concurrentProcess,
	localActions *LocalActionsCache,
	localReusableWorkflows *LocalReusableWorkflowCache,
	usedRules *[]Rule,
) ([]*Error, error) {
	// Each call owns its rules; caches and process scheduling are shared across files.

	var start time.Time
	if l.logLevel >= LogLevelVerbose {
		start = time.Now()
	}

	l.log("Linting", path)
	if project != nil {
		l.log("Using project at", project.RootDir())
	}

	if cfg != nil {
		l.debug("Config: %#v", cfg)
	} else {
		l.debug("No config was found")
	}

	w, all := Parse(content)

	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found", len(all), "parse errors in", elapsed.Milliseconds(), "ms for", path)
	}

	if w != nil {
		dbg := l.debugWriter()
		localActions = &LocalActionsCache{base: localActions}

		rules := []Rule{}
		c := ruleContext{path: path, config: cfg, actions: localActions, workflows: localReusableWorkflows, process: proc, shellcheck: l.shellcheck, pyflakes: l.pyflakes}
		c.shellcheckOptions, c.pyflakesOptions = l.shellcheckOptions, l.pyflakesOptions
		c.shellcheckSettings = l.shellcheckSettings
		c.workingDir, c.inputs = l.workingDir, l.inputs
		c.gitModes = l.gitModes
		if project != nil {
			c.projectRoot = project.RootDir()
		}
		for _, descriptor := range builtinRuleDescriptors() {
			if descriptor.build == nil {
				continue
			}
			if descriptor.enabled != nil && !descriptor.enabled(c) {
				if descriptor.Category == "external" {
					if descriptor.Name == "shellcheck" && c.config != nil && c.config.Tools.Shellcheck.Enabled != nil && !*c.config.Tools.Shellcheck.Enabled {
						l.log(`Rule "shellcheck" was disabled by tools.shellcheck.enabled`)
					} else {
						l.log(fmt.Sprintf("Rule %q was disabled since %s command name was empty", descriptor.Name, descriptor.Name))
					}
				}
				continue
			}
			r, err := descriptor.build(c)
			if err != nil {
				l.log(fmt.Sprintf("Rule %q was disabled:", descriptor.Name), err)
				continue
			}
			rules = append(rules, r)
		}

		if l.onRulesCreated != nil {
			rules = l.onRulesCreated(rules)
		}

		v := NewVisitor()
		v.actions = localActions
		for _, rule := range rules {
			v.AddPass(rule)
		}
		if dbg != nil {
			v.EnableDebug(dbg)
			for _, r := range rules {
				r.EnableDebug(dbg)
			}
		}
		if cfg != nil {
			for _, r := range rules {
				r.SetConfig(cfg)
			}
		}

		if err := v.Visit(w); err != nil {
			l.debug("Error occurred while visiting workflow syntax tree: %v", err)
			return nil, err
		}

		for _, rule := range rules {
			errs := rule.Errs()
			l.debug("%s found %d errors", rule.Name(), len(errs))
			all = append(all, errs...)
		}
		for _, composite := range v.compositeRules {
			for _, rule := range composite.rules {
				for _, finding := range rule.Errs() {
					if finding.Filepath == "" {
						finding.Filepath = composite.meta.Path()
						finding.source = composite.meta.src
					}
					all = append(all, finding)
				}
			}
		}

		*usedRules = rules
	}

	var suppressionPolicy *SuppressionsPolicy
	if cfg != nil {
		suppressionPolicy = cfg.Policy.DisallowSuppressions
	}
	all = filterInlineSuppressions(content, all, suppressionPolicy)
	byPath := make(map[string][]*Error)
	for _, finding := range all {
		findingPath := finding.Filepath
		if findingPath == "" {
			findingPath = path
		} else if project != nil && filepath.IsAbs(findingPath) {
			if relative, err := filepath.Rel(project.RootDir(), findingPath); err == nil {
				findingPath = relative
			}
		}
		byPath[findingPath] = append(byPath[findingPath], finding)
	}
	all = nil
	for findingPath, findings := range byPath {
		all = append(all, l.filterErrors(findings, cfg.PathConfigs(findingPath))...)
	}

	for _, err := range all {
		if err.Filepath == "" {
			err.Filepath = path // Populate filename in the error
		}
	}

	slices.SortFunc(all, compareErrors)
	all = slices.CompactFunc(all, equalsErrors) // Alias may duplicate errors

	if l.logLevel >= LogLevelVerbose {
		elapsed := time.Since(start)
		l.log("Found total", len(all), "errors in", elapsed.Milliseconds(), "ms for", path)
	}

	return all, nil
}

func (l *analysisEngine) filterErrors(errs []*Error, cfgs []PathConfig) []*Error {
	if len(l.ignorePats) == 0 && len(cfgs) == 0 {
		return errs
	}

	filtered := make([]*Error, 0, len(errs))
Loop:
	for _, err := range errs {
		if l.ignorePats.Match(err) {
			l.debug("Error %q is ignored due to -ignore command line option", err.Message)
			continue Loop
		}
		for _, c := range cfgs {
			if c.Ignore.Match(err) {
				l.debug("Error %q is ignored due to the \"ignore\" config in the config file", err.Message)
				continue Loop
			}
		}
		filtered = append(filtered, err)
	}
	if len(filtered) != len(errs) {
		l.log("Filtered", len(errs)-len(filtered), "error(s) due to \"-ignore\" command line option and \"ignore\" configuration")
	}
	return filtered
}
