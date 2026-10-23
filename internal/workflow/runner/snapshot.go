package runner

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	runnerapi "actionlint.kjanat.dev/v2/runner"
)

// snapshotProvider resolves GitHub selectors against an explicitly selected inventory.
type snapshotProvider struct{}

type runnerInventory struct {
	Runners []inventoryRunner `json:"runners"`
}

type inventoryRunner struct {
	Name            string                    `json:"name"`
	Labels          []string                  `json:"labels"`
	Group           string                    `json:"group"`
	OS              runnerapi.OS              `json:"os"`
	Architecture    string                    `json:"architecture"`
	DefaultShell    string                    `json:"default_shell"`
	CaseSensitivity runnerapi.CaseSensitivity `json:"case_sensitivity"`
	ShIsDash        bool                      `json:"sh_is_dash"`
	Ephemeral       bool                      `json:"ephemeral"`
}

func (snapshotProvider) RunnerDescriptor() runnerapi.Descriptor {
	return runnerapi.Descriptor{Name: "snapshot", Version: "1", Targets: []string{"github"}}
}

func (snapshotProvider) ResolveRunners(ctx context.Context, request runnerapi.Request) ([]runnerapi.Answer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var inventory runnerInventory
	if request.Target == "github" && request.Snapshot.Content != "" {
		if err := jsonv2.Unmarshal([]byte(request.Snapshot.Content), &inventory, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, fmt.Errorf("decode runner inventory: %w", err)
		}
		if err := inventory.validate(); err != nil {
			return nil, err
		}
	}
	evidence := runnerapi.Evidence{
		Dataset: "snapshot:" + request.Snapshot.Identity(), Scope: request.Snapshot.Scope,
		ObtainedAt: request.Snapshot.ObtainedAt, Freshness: request.Snapshot.Freshness, Complete: request.Snapshot.Complete,
	}
	// An absent snapshot gives unknown results in the requested scope.
	if request.Snapshot.Content == "" {
		evidence.Scope, evidence.Freshness, evidence.Complete = request.Scope, runnerapi.Missing, false
	}
	answers := make([]runnerapi.Answer, 0, len(request.Selectors))
	for _, selector := range request.Selectors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var resolution runnerapi.Resolution
		switch {
		case request.Target != "github":
			resolution = runnerapi.NotApplicable{}
		case evidence.Scope != request.Scope:
			return nil, errors.New("runner inventory scope does not match requested scope")
		case evidence.Freshness != runnerapi.Fresh || !evidence.Complete:
			resolution = runnerapi.Unknown{Reason: "runner inventory is missing, stale or incomplete"}
		case unresolvedSelector(selector):
			resolution = runnerapi.Unknown{Reason: "runner selector depends on runtime values"}
		default:
			resolution = inventory.resolve(selector)
		}
		answers = append(answers, runnerapi.Answer{Resolution: resolution, Evidence: evidence})
	}
	return answers, nil
}

func (inventory runnerInventory) validate() error {
	if inventory.Runners == nil {
		return errors.New("runner inventory requires a runners array")
	}
	names := make(map[string]bool, len(inventory.Runners))
	for _, r := range inventory.Runners {
		if strings.TrimSpace(r.Name) == "" || names[r.Name] {
			return fmt.Errorf("runner inventory contains an empty or duplicate name %q", r.Name)
		}
		names[r.Name] = true
		switch r.OS {
		case "", runnerapi.UnknownOS, runnerapi.Linux, runnerapi.MacOS, runnerapi.Windows:
		default:
			return fmt.Errorf("runner %q has invalid OS %q", r.Name, r.OS)
		}
		switch r.CaseSensitivity {
		case "", runnerapi.UnknownCase, runnerapi.CaseSensitive, runnerapi.CaseInsensitive:
		default:
			return fmt.Errorf("runner %q has invalid case sensitivity %q", r.Name, r.CaseSensitivity)
		}
		for _, label := range r.Labels {
			if strings.TrimSpace(label) == "" {
				return fmt.Errorf("runner %q has an empty label", r.Name)
			}
		}
	}
	return nil
}

func unresolvedSelector(selector runnerapi.Selector) bool {
	if selector.Incomplete || selector.Expression != "" {
		return true
	}
	if selector.Group != nil {
		if _, ok := selector.Group.Value.(runnerapi.Literal); !ok {
			return true
		}
	}
	for _, constraint := range selector.Labels {
		if _, ok := constraint.Value.(runnerapi.Literal); !ok {
			return true
		}
	}
	return false
}

func (inventory runnerInventory) resolve(selector runnerapi.Selector) runnerapi.Resolution {
	var candidates []runnerapi.Candidate
	for _, r := range inventory.Runners {
		if !r.matches(selector) {
			continue
		}
		os, caseSensitivity := r.OS, r.CaseSensitivity
		if os == "" {
			os = runnerapi.UnknownOS
		}
		if caseSensitivity == "" {
			caseSensitivity = runnerapi.UnknownCase
		}
		candidates = append(candidates, runnerapi.Candidate{
			Name: r.Name, OS: os, Architecture: r.Architecture, DefaultShell: r.DefaultShell,
			CaseSensitivity: caseSensitivity, ShIsDash: r.ShIsDash, Ephemeral: r.Ephemeral,
		})
	}
	if len(candidates) == 0 {
		return runnerapi.NoMatch{Rejections: []runnerapi.Rejection{{Reason: "no runner matches all labels and group in the selected inventory"}}}
	}
	return runnerapi.Resolved{Candidates: candidates}
}

func (r inventoryRunner) matches(selector runnerapi.Selector) bool {
	if selector.Group != nil && selector.Group.Value != runnerapi.Literal(r.Group) {
		return false
	}
	for _, constraint := range selector.Labels {
		found := false
		for _, label := range r.Labels {
			if value, ok := constraint.Value.(runnerapi.Literal); ok && strings.EqualFold(string(value), label) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
