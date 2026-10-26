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
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/engine"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	"actionlint.kjanat.dev/v2/internal/fsread"
	scriptexecution "actionlint.kjanat.dev/v2/internal/tools/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type Options struct {
	Budget          *invocation.Budget
	SourceSnapshots *documentparse.SourceSnapshots
	Runners         *runnerlang.Knowledge
	// ScriptInputs merge with per-config selectors; conflicting languages are errors.
	ScriptInputs []analyzer.ScriptInput
	// ScriptInputsForConfig loads lazily once per immutable config, including nil.
	ScriptInputsForConfig func(*repoconfig.Config) ([]analyzer.ScriptInput, error)
	Context               context.Context
	Actions               *actionlocal.Resolver
	Rules                 []workflowvisit.Rule
	Scripts               scriptexecution.Service
	Paths                 execution.Paths
	Config                *repoconfig.Config
	// ConfigForFile resolves each physical manifest once, including no-script scopes.
	ConfigForFile func(string) (*repoconfig.Config, error)
	// CollectScripts supplies execution units. Registration, selector errors and
	// script budgets apply even when no script analyzer consumes the occurrences.
	CollectScripts, ResolveExecution bool
	Join                             func() error
	Debug                            io.Writer
}

type Result struct {
	Findings         []*findingmodel.Finding
	Batch            scriptexecution.Batch
	ResolveExecution bool
	Sources          map[string][]byte
	// Configs is keyed by expanded source path.
	Configs map[string]*repoconfig.Config
}

type Service struct{ options Options }

func New(options Options) *Service { return &Service{options: options} }

// Workflow enters the workflow pass lifecycle. Action never uses this path.
func (s *Service) Workflow(snapshot *documentparse.SourceSnapshot) (Result, error) {
	return s.analyze(func(c *compositeAnalyzer) error {
		if snapshot == nil {
			return errors.New("workflow source snapshot is unavailable")
		}
		w, available := snapshot.Workflow()
		if !available {
			return errors.New("workflow source snapshot has no workflow declarations")
		}
		if err := c.startEntry(invocation.EntryWorkflow, snapshot); err != nil {
			return err
		}
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
	return s.analyze(func(c *compositeAnalyzer) error {
		if metadata == nil {
			return errors.New("action metadata is unavailable")
		}
		if c.sourceSnapshots == nil {
			c.sourceSnapshots = &documentparse.SourceSnapshots{}
		}
		snapshot, err := c.sourceSnapshots.Action(fsread.PhysicalPath(metadata.Path()), metadata)
		if err != nil {
			return err
		}
		if err := c.startEntry(invocation.EntryAction, snapshot); err != nil {
			return err
		}
		return visitStandaloneAction(metadata, s.options.Rules, c)
	})
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
		budget:                invocation.DefaultBudget(),
		scriptInputs:          scriptInputs,
		scriptInputsForConfig: options.ScriptInputsForConfig,
		scriptCatalogues:      map[*repoconfig.Config][]analyzer.ScriptInput{},
		sourceSnapshots:       options.SourceSnapshots,
	}
	if options.Budget != nil {
		c.budget = *options.Budget
	}
	if options.ConfigForFile != nil {
		physicalConfigs := map[string]*repoconfig.Config{}
		c.configForFile = func(path string) (*repoconfig.Config, error) {
			physical := fsread.PhysicalPath(path)
			if config, exists := physicalConfigs[physical]; exists {
				configs[path] = config
				return config, nil
			}
			config, err := options.ConfigForFile(path)
			if err != nil {
				return nil, fmt.Errorf("load configuration for action %q: %w", path, err)
			}
			physicalConfigs[physical], configs[path] = config, config
			return config, nil
		}
	}
	err = visit(c)
	err = c.interrupt(err)
	var graph invocation.Graph
	if c.invocations != nil {
		var publishErr error
		graph, publishErr = c.invocations.Publish()
		err = errors.Join(err, publishErr)
	}
	var batch scriptexecution.Batch
	var findings []*findingmodel.Finding
	if graph.Has(graph.RootID()) {
		var prepareErr error
		batch, prepareErr = scriptexecution.NewBatch(graph, scriptexecution.Collection{
			Declarations: c.sourceSnapshots, Scopes: c.scriptScopes, Scripts: c.scripts, Collected: c.collectScripts,
		})
		err = errors.Join(err, prepareErr)
		if prepareErr == nil {
			var scriptErr error
			findings, scriptErr = options.Scripts.Run(options.Context, batch)
			err = errors.Join(err, scriptErr)
		}
	}
	err = errors.Join(err, join())
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
	return Result{Findings: findings, Batch: batch, ResolveExecution: c.resolveScriptExecution, Sources: options.Actions.Sources(), Configs: configs}, err
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
