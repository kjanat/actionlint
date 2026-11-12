// Package runners acquires runner-provider answers for captured workflows.
package runners

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/model/parsing"
	runner "actionlint.kjanat.dev/v2/internal/model/runners"
	runnerapi "actionlint.kjanat.dev/v2/internal/model/runners/contract"
)

// Prepare acquires runner answers with an explicitly known custom-label list.
func Prepare(ctx context.Context, snapshot parsing.SourceSnapshot, custom []string, options runnerapi.Options, scope string) (*runner.Knowledge, error) {
	return PrepareWithLabels(ctx, snapshot, runner.KnownLabels(custom), options, scope)
}

// PrepareWithLabels selects and invokes a provider before constructing job knowledge.
// Failed acquisition returns unknown capabilities with any established provider identity.
func PrepareWithLabels(ctx context.Context, snapshot parsing.SourceSnapshot, custom runner.LabelSet, options runnerapi.Options, scope string) (*runner.Knowledge, error) {
	if options.Target == "" {
		options.Target = "github"
	}
	unknown := runner.UnknownKnowledge(options.Target)
	if !snapshot.Present() {
		return unknown, nil
	}
	if _, available := snapshot.WorkflowView(); !available {
		return unknown, nil
	}
	hasSnapshot := options.Snapshot != (runnerapi.Snapshot{})
	switch options.Snapshot.Freshness {
	case "":
		options.Snapshot.Freshness = runnerapi.Missing
	case runnerapi.Fresh, runnerapi.Stale, runnerapi.Missing:
	default:
		return unknown, errors.New("invalid runner snapshot freshness")
	}
	if options.Scope != "" {
		scope = options.Scope
	}
	if hasSnapshot && options.Snapshot.Scope != scope {
		return unknown, errors.New("runner snapshot scope does not match requested scope")
	}
	var selected runnerapi.Provider
	selection := runner.ProviderSelection{}
	if options.Target == "github" && (options.Provider == "" || options.Provider == "github") {
		selected = runner.GitHubProvider(custom)
		selection.BuiltinLabels, selection.ImplementationID = true, "builtin-github"
	}
	if options.Target == "github" && options.Provider == "snapshot" {
		selected = runner.SnapshotProvider()
		selection.ImplementationID = "builtin-github-snapshot-v1"
	}
	for _, registration := range options.Providers {
		if registration.Provider == nil {
			return unknown, errors.New("nil runner provider")
		}
		descriptor := registration.Provider.RunnerDescriptor()
		if options.Target == "github" && options.Provider == "" {
			continue
		}
		if !slices.Contains(descriptor.Targets, options.Target) || options.Provider != "" && descriptor.Name != options.Provider {
			continue
		}
		if selected != nil {
			return unknown, fmt.Errorf("conflicting runner providers for target %q; select one explicitly", options.Target)
		}
		selected, selection.ImplementationID = registration.Provider, registration.ImplementationID
	}
	if selected != nil {
		selection.Descriptor = selected.RunnerDescriptor()
	}
	request := runnerapi.Request{Target: options.Target, Scope: scope, Snapshot: options.Snapshot}
	plan, err := runner.NewResolutionPlan(snapshot, custom, request, selection, hasSnapshot)
	if err != nil {
		return unknown, err
	}
	if plan.Empty() {
		return unknown, nil
	}
	if selected == nil {
		return unknown, fmt.Errorf("no runner provider %q for target %q", options.Provider, options.Target)
	}
	answers, err := selected.ResolveRunners(ctx, plan.Request())
	if err != nil {
		return plan.Unknown(), fmt.Errorf("runner provider %s: %w", selection.Descriptor.Name, err)
	}
	return plan.Complete(answers)
}
