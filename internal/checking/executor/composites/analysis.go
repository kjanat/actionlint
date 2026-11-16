// Package composites checks workflow and action invocations with prepared rules.
// It owns nested discovery, script facts and completion, returning stable values.
package composites

import (
	"context"
	"errors"
	"fmt"
	"io"

	"actionlint.kjanat.dev/v2/internal/checking/binding"
	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"
	scriptexecution "actionlint.kjanat.dev/v2/internal/checking/scripts"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/workflowvisit"
	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/files"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/input/source/capture"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runnerlang "actionlint.kjanat.dev/v2/internal/model/runners"
	"actionlint.kjanat.dev/v2/internal/model/workflows/execution"
	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

type Options struct {
	Bindings        *binding.Scope
	BindSource      func(*binding.Scope, string) (*binding.Scope, error)
	Budget          *invocation.Budget
	SourceSnapshots *parsing.SourceSnapshots
	Captures        *capture.Store
	RemoteActions   *remote.Service
	// OnRemoteActionDocument receives each remote action manifest this
	// composition captures, with the document parsed from the captured bytes.
	OnRemoteActionDocument func(capture.CapturedDocument, parsing.Document)
	// PrepareSource acquires only the supplied references before checks observe the source.
	PrepareSource func(parsing.SourceSnapshot, []string) error
	Runners       *runnerlang.Knowledge
	// ScriptInputs merge with per-config selectors; conflicting languages are errors.
	ScriptInputs []checkinputs.ScriptInput
	// ScriptInputsForScope loads lazily once per immutable config, including nil.
	ScriptInputsForScope func(*binding.Scope) ([]checkinputs.ScriptInput, error)
	Context              context.Context
	Actions              *workspaceactions.Resolver
	Rules                []workflowvisit.Rule
	Scripts              scriptexecution.Service
	Paths                execution.Paths
	Reader               *files.Reader // records reads made while resolving script paths

	// ConfigProvenance is shared by the entry settings and per-file projections.
	ConfigProvenance configuration.Provenance
	// CollectScripts supplies execution units. Registration, selector errors and
	// script budgets apply even when no script analyzer consumes the occurrences.
	CollectScripts, ResolveExecution bool
	Join                             func() error
	Debug                            io.Writer
}

// Result is a plain record of one composition run. It holds no invariants of
// its own; CheckInput validates its batch against the entry snapshot.
type Result struct {
	IncompleteChecks []report.IncompleteCheck
	Findings         []*findingmodel.Finding
	EntrySnapshot    parsing.SourceSnapshot
	Batch            scriptexecution.Batch
	// Invocations is available after graph publication even if batch preparation fails.
	Invocations      invocation.Graph
	ResolveExecution bool
	Sources          map[string][]byte
	SourceDocuments  map[string]SourceDocument
}

type SourceDocument interface {
	Capture() capture.CapturedDocument
	Content() []byte
	Provenance() parsing.Provenance
}

// Workflow runs one workflow composition with the caller's collaborators.
// It enters the workflow pass lifecycle; Action never uses this path.
// Options.Join runs exactly once per call.
func Workflow(options Options, snapshot parsing.SourceSnapshot) (Result, error) {
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
		return workflowvisit.Visit(w, workflowvisit.Options{
			Passes: c.passes,
			Hooks:  workflowvisit.Hooks{Workflow: c.beginWorkflow, Job: c.beginJob, JobPass: c.visitJobPass, Step: c.visitStep, ParallelBoundary: c.invalidateCheckout},
			Debug:  options.Debug,
		})
	})
}

// Action runs one standalone action composition with the caller's collaborators.
// Options.Join runs exactly once per call.
func Action(options Options, snapshot parsing.SourceSnapshot, deployment scriptexecution.Deployment, metadata *actionmeta.Metadata) (Result, error) {
	return analyze(options, func(c *compositeAnalyzer) error {
		if snapshot.Kind() != parsing.KindAction {
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
	c := &compositeAnalyzer{
		bindings:     options.Bindings,
		scriptConfig: options.Bindings.Composite(),
		actions:      options.Actions,
		compositionSession: &compositionSession{
			bindSource: options.BindSource,
			ctx:        options.Context, passes: passes,
			runners:          options.Runners,
			scriptPaths:      options.Paths,
			reader:           options.Reader,
			configProvenance: options.ConfigProvenance,
			collectScripts:   options.CollectScripts, resolveScriptExecution: options.ResolveExecution,
			budget:               invocation.DefaultBudget(),
			scriptInputs:         scriptInputs,
			scriptInputsForScope: options.ScriptInputsForScope,
			scriptCatalogues:     map[*binding.Scope][]checkinputs.ScriptInput{},
			sourceSnapshots:      options.SourceSnapshots,
			captures:             options.Captures,
			remoteActions:        options.RemoteActions,
			onRemoteDocument:     options.OnRemoteActionDocument,
			remoteDocuments:      map[string]remoteDocument{},
			metadataCopies:       findingmodel.NewCopier(),
			remoteLookups:        map[actions.Reference]struct{}{},
			prepareSource:        options.PrepareSource,
		},
	}
	if options.Budget != nil {
		c.budget = *options.Budget
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
	sources := options.Actions.Sources()
	documents := map[string]SourceDocument{}
	for path, document := range options.Actions.SourceDocuments() {
		documents[path] = document
	}
	for path, document := range c.remoteDocuments {
		documents[path], sources[path] = document, document.Content()
	}
	return Result{IncompleteChecks: c.incompleteChecks, Findings: findings, EntrySnapshot: c.sourceSnapshot, Batch: batch, Invocations: graph, ResolveExecution: c.resolveScriptExecution, Sources: sources, SourceDocuments: documents}, err
}

func scriptInputCatalogue(additional []checkinputs.ScriptInput) ([]checkinputs.ScriptInput, error) {
	// Multiple engines may analyze the same site, provided they agree on its language.
	known := checkinputs.ScriptInput{Action: "actions/github-script", Input: "script", Language: "javascript"}
	inputs := []checkinputs.ScriptInput{known}
	languages := map[[2]string]string{{known.Action, known.Input}: known.Language}
	for _, input := range additional {
		if err := checkinputs.ValidateScriptInput(input); err != nil {
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
