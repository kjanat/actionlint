// Package composition analyzes workflow and action invocations with prepared checks.
// It owns nested discovery, script facts and completion, returning stable values.
package composition

import (
	"context"
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/analyzer"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/checks"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/engine"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type Options struct {
	Runners                          *runnerlang.Knowledge
	ScriptInputs                     []analyzer.ScriptInput
	ScriptInputsForConfig            func(*repoconfig.Config) ([]analyzer.ScriptInput, error)
	Context                          context.Context
	Actions                          *actionlocal.Resolver
	Rules                            []workflowvisit.Rule
	Scripts                          scriptexecution.Service
	Paths                            execution.Paths
	Config                           *repoconfig.Config
	ConfigForFile                    func(string) (*repoconfig.Config, error)
	CollectScripts, ResolveExecution bool
	Join                             func() error
	Debug                            io.Writer
}

type Result struct {
	Findings                         []*findingmodel.Finding
	Batch                            scriptexecution.Batch
	CollectScripts, ResolveExecution bool
	Sources                          map[string][]byte
	Configs                          map[string]*repoconfig.Config
}

type Service struct{ options Options }

func New(options Options) *Service { return &Service{options: options} }

// Workflow enters the workflow pass lifecycle. Action never uses this path.
func (s *Service) Workflow(w *workflowlang.Workflow) (Result, error) {
	return s.analyze(func(c *compositeAnalyzer) error {
		v := engine.NewVisitor()
		v.Hooks = engine.Hooks{Workflow: c.beginWorkflow, Job: c.beginJob, Step: c.visitStep, ParallelBoundary: c.invalidateCheckout}
		for _, pass := range s.options.Rules {
			v.AddPass(checks.WithOrigins(pass, s.options.Config))
		}
		v.EnableDebug(s.options.Debug)
		return v.Visit(w)
	})
}

func (s *Service) Action(metadata *actionmeta.Metadata) (Result, error) {
	return s.analyze(func(c *compositeAnalyzer) error { return visitStandaloneAction(metadata, s.options.Rules, c) })
}

func (s *Service) analyze(visit func(*compositeAnalyzer) error) (result Result, err error) {
	options := s.options
	// Join even when a visit fails. Collection happens only after all started work ends.
	joined := false
	join := func() error {
		if options.Join != nil {
			return options.Join()
		}
		return nil
	}
	defer func() {
		if !joined {
			err = errors.Join(err, join())
		}
	}()
	scriptInputs, err := scriptInputCatalogue(options.ScriptInputs)
	if err != nil {
		return result, err
	}
	passes := make([]workflowvisit.Pass, len(options.Rules))
	for i, r := range options.Rules {
		passes[i] = r
	}
	configs := map[string]*repoconfig.Config{}
	c := &compositeAnalyzer{
		ctx: options.Context, actions: options.Actions, passes: passes,
		runners:      options.Runners,
		scriptConfig: options.Config, scriptPaths: options.Paths,
		collectScripts: options.CollectScripts, resolveScriptExecution: options.ResolveExecution,
		scriptInputs:          scriptInputs,
		scriptInputsForConfig: options.ScriptInputsForConfig,
	}
	if options.ConfigForFile != nil {
		c.configForFile = func(path string) (*repoconfig.Config, error) {
			config, err := options.ConfigForFile(path)
			if err == nil {
				configs[path] = config
			}
			return config, err
		}
	}
	err = visit(c)
	batch := scriptexecution.Batch{Scopes: c.scriptScopes, Scripts: c.scripts}
	findings, scriptErr := options.Scripts.Run(options.Context, batch)
	err = errors.Join(err, scriptErr, join())
	joined = true
	for _, r := range options.Rules {
		findings = append(findings, r.Errs()...)
	}
	for _, composite := range c.rules {
		for _, r := range composite.rules {
			for _, finding := range r.Errs() {
				if origin := finding.SuppressionOrigin; origin != nil && origin.Path == "" {
					origin.Path = composite.meta.Path()
				}
				if finding.Path == "" {
					finding.Path, finding.Source = composite.meta.Path(), composite.meta.Source
				}
				findings = append(findings, finding)
			}
		}
	}
	return Result{Findings: findings, Batch: batch, CollectScripts: c.collectScripts, ResolveExecution: c.resolveScriptExecution, Sources: options.Actions.Sources(), Configs: configs}, err
}

func scriptInputCatalogue(additional []analyzer.ScriptInput) ([]analyzer.ScriptInput, error) {
	// Multiple engines may analyze the same site, provided they agree on its language.
	known := analyzer.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}
	inputs := []analyzer.ScriptInput{known}
	languages := map[[2]string]string{{known.Action, known.Input}: known.Language}
	for _, input := range additional {
		if err := analyzer.ValidateScriptInput(input); err != nil {
			return nil, err
		}
		key := [2]string{input.Action, input.Input}
		if language, found := languages[key]; found {
			if input.Language != language {
				return nil, fmt.Errorf("script input %s/%s has conflicting languages %q and %q", input.Action, input.Input, language, input.Language)
			}
			continue
		}
		languages[key] = input.Language
		inputs = append(inputs, input)
	}
	return inputs, nil
}
