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
	Reader                *fsread.Reader // records reads made while resolving script paths
	Config                *repoconfig.Config
	// ConfigForFile resolves each logical manifest path once, including no-script scopes.
	ConfigForFile func(string) (*repoconfig.Config, error)
	// CollectScripts supplies execution units. Registration, selector errors and
	// script budgets apply even when no script analyzer consumes the occurrences.
	CollectScripts, ResolveExecution bool
	Join                             func() error
	Debug                            io.Writer
}

// Result is a plain record of one composition run. It holds no invariants of
// its own; AnalyzerInput validates its batch against the entry snapshot.
type Result struct {
	Findings         []*findingmodel.Finding
	EntrySnapshot    documentparse.SourceSnapshot
	Batch            scriptexecution.Batch
	ResolveExecution bool
	Sources          map[string][]byte
	SourceDocuments  map[string]actionlocal.SourceDocument
	// Configs is keyed by logical manifest path.
	Configs map[string]*repoconfig.Config
}

// Workflow runs one workflow composition with the caller's collaborators.
// It enters the workflow pass lifecycle; Action never uses this path.
// Options.Join runs exactly once per call.
func Workflow(options Options, snapshot documentparse.SourceSnapshot) (Result, error) {
	return analyze(options, func(c *compositeAnalyzer) error {
		if !snapshot.Present() {
			return errors.New("workflow source snapshot is unavailable")
		}
		w, available := snapshot.WorkflowView()
		if !available {
			return errors.New("workflow source snapshot has no workflow declarations")
		}
		if err := c.startEntry(invocation.EntryWorkflow, snapshot); err != nil {
			return err
		}
		return engine.Visit(w, engine.Options{
			Passes: c.passes,
			Hooks:  engine.Hooks{Workflow: c.beginWorkflow, Job: c.beginJob, JobPass: c.visitJobPass, Step: c.visitStep, ParallelBoundary: c.invalidateCheckout},
			Debug:  options.Debug,
		})
	})
}

// Action runs one standalone action composition with the caller's collaborators.
// Options.Join runs exactly once per call.
func Action(options Options, snapshot documentparse.SourceSnapshot, deployment scriptexecution.Deployment, metadata *actionmeta.Metadata) (Result, error) {
	return analyze(options, func(c *compositeAnalyzer) error {
		if snapshot.Kind() != documentparse.KindAction {
			return errors.New("action source snapshot is unavailable")
		}
		if metadata == nil {
			return errors.New("action metadata is unavailable")
		}
		if err := c.startEntry(invocation.EntryAction, snapshot); err != nil {
			return err
		}
		return visitStandaloneAction(metadata, deployment, options.Rules, c)
	})
}

func analyze(options Options, visit func(*compositeAnalyzer) error) (result Result, err error) {
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
		scriptConfig: options.Config,
		actions:      options.Actions,
		compositionSession: &compositionSession{
			ctx: options.Context, passes: passes,
			runners:        options.Runners,
			scriptPaths:    options.Paths,
			reader:         options.Reader,
			collectScripts: options.CollectScripts, resolveScriptExecution: options.ResolveExecution,
			budget:                invocation.DefaultBudget(),
			scriptInputs:          scriptInputs,
			scriptInputsForConfig: options.ScriptInputsForConfig,
			scriptCatalogues:      map[*repoconfig.Config][]analyzer.ScriptInput{},
			sourceSnapshots:       options.SourceSnapshots,
		},
	}
	if options.Budget != nil {
		c.budget = *options.Budget
	}
	if options.ConfigForFile != nil {
		c.configForFile = func(path string) (*repoconfig.Config, error) {
			if config, exists := configs[path]; exists {
				return config, nil
			}
			config, err := options.ConfigForFile(path)
			if err != nil {
				return nil, fmt.Errorf("load configuration for action %q: %w", path, err)
			}
			configs[path] = config
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
	findings = append(findings, c.metadataFindings...)
	return Result{Findings: findings, EntrySnapshot: c.sourceSnapshot, Batch: batch, ResolveExecution: c.resolveScriptExecution, Sources: options.Actions.Sources(), SourceDocuments: options.Actions.SourceDocuments(), Configs: configs}, err
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
