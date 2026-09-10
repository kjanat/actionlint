package actionlint

import (
	"context"
	"fmt"
	"slices"
	"time"
)

type analysisEngine struct {
	analysisLogger
	ctx                  context.Context
	shellcheck, pyflakes string
	ignorePats           IgnorePatterns
	onRulesCreated       func([]Rule) []Rule
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

		rules := []Rule{}
		c := ruleContext{path: path, config: cfg, actions: localActions, workflows: localReusableWorkflows, process: proc, shellcheck: l.shellcheck, pyflakes: l.pyflakes}
		for _, descriptor := range builtinRuleDescriptors() {
			if descriptor.build == nil {
				continue
			}
			if descriptor.enabled != nil && !descriptor.enabled(c) {
				if descriptor.Category == "external" {
					l.log(fmt.Sprintf("Rule %q was disabled since %s command name was empty", descriptor.Name, descriptor.Name))
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

		*usedRules = rules
	}

	all = l.filterErrors(all, cfg.PathConfigs(path))

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
