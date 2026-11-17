// Package rulebinding prepares rule configuration once for execution and publication.
package rulebinding

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/input/configuration"
	"actionlint.kjanat.dev/v2/internal/input/configuration/ruleconfig"
	"actionlint.kjanat.dev/v2/internal/model/binding"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

// Rule is a prepared configuration decision. Execution and publication retain
// the same record; publication never selects a configuration scope again.
type Rule struct {
	scope      *Scope
	id         binding.ID
	execution  *configuration.Config
	policy     *configuration.Config
	options    json.RawMessage
	optionsErr error
}

// Options returns caller-owned analyzer options. Explicit registration options take precedence.
func (r *Rule) Options(explicit json.RawMessage) (json.RawMessage, error) {
	if len(explicit) != 0 {
		return slices.Clone(explicit), nil
	}
	return slices.Clone(r.options), r.optionsErr
}

// ID identifies the decision within its analysis registry. Zero is unbound.
func (r *Rule) ID() binding.ID { return r.id }

// Registry owns every prepared decision for one entry analysis.
type Registry struct {
	rules     configuration.RuleSet
	records   []*Rule
	namespace binding.Namespace
	sealed    bool
}

// SealedRegistry permits publication to read only decisions prepared before sealing.
type SealedRegistry struct {
	namespace binding.Namespace
	records   []*Rule
}

// Seal ends preparation. Existing scopes remain readable by completed tasks.
func (r *Registry) Seal() *SealedRegistry {
	r.sealed = true
	return &SealedRegistry{namespace: r.namespace, records: r.records}
}

// NewRegistry creates an analysis-owned configuration binding registry.
func NewRegistry(rules configuration.RuleSet) *Registry {
	return &Registry{rules: rules, namespace: binding.NewNamespace()}
}

func (r *Registry) add(execution, policy *configuration.Config) *Rule {
	decision := &Rule{id: r.namespace.ID(uint64(len(r.records)) + 1), execution: execution, policy: policy}
	r.records = append(r.records, decision)
	return decision
}

// Lookup returns a prepared decision without selecting configuration scope.
func (r *SealedRegistry) Lookup(id binding.ID) (*Rule, bool) {
	if r == nil {
		return nil, false
	}
	index, valid := r.namespace.Index(id)
	if !valid || index > uint64(len(r.records)) {
		return nil, false
	}
	return r.records[index-1], true
}

// Config returns the resolved execution settings. Consumers must not mutate them.
func (r *Rule) Config() *configuration.Config { return r.execution }

// Policy returns resolved notification settings, including an entry lint-off override.
func (r *Rule) Policy() *configuration.Config { return r.policy }

// Scope returns the occurrence's prepared configuration roles.
func (r *Rule) Scope() *Scope { return r.scope }

// Scope owns the prepared rule decisions for one source within an entry analysis.
// Its maps are private and are never changed after construction.
type Scope struct {
	rules     configuration.RuleSet
	bindings  map[string]*Rule
	fallback  *Rule
	source    *configuration.Config
	composite *configuration.Config
	caller    *configuration.Config
}

// Prepare binds all declared rules. The caller owns configuration resolution;
// source and composite projections can differ even for the same document.
func (r *Registry) Prepare(caller, source, composite *configuration.Config) *Scope {
	if r.sealed {
		panic("configuration binding registry is sealed")
	}
	s := &Scope{rules: r.rules, source: source, composite: composite, caller: caller, bindings: make(map[string]*Rule)}
	s.fallback = r.add(caller, caller)
	s.fallback.scope = s
	for _, declaration := range r.rules.Rules() {
		selected := caller
		switch declaration.Scope {
		case rule.SourceConfiguration:
			selected = source
		case rule.CompositeConfiguration:
			selected = composite
		case rule.CallerConfiguration:
		}
		policy := selected
		if caller != nil && caller.Lint.Enabled != nil && !*caller.Lint.Enabled {
			policy = caller
		}
		decision := r.add(selected, policy)
		decision.scope = s
		if selected != nil {
			options, err := selected.Lint.Rules.RuleOptions(declaration.Name)
			if err == nil && options != nil {
				decision.options, err = jsonv2.Marshal(options)
			}
			if err != nil {
				decision.optionsErr = fmt.Errorf("options for analyzer %q: %w", declaration.Name, err)
			}
		}
		s.bindings[declaration.Name] = decision
	}
	return s
}

// Rule returns an existing binding. Undeclared diagnostics use the prepared caller binding.
func (s *Scope) Rule(name string) *Rule {
	if r, exists := s.bindings[s.rules.ResolveAlias(name)]; exists {
		return r
	}
	return s.fallback
}

// DiagnosticLevel reads the policy selected when this occurrence was prepared.
func (s *Scope) DiagnosticLevel(name string) ruleconfig.Level {
	return s.rules.DiagnosticLevel(s.Rule(name).policy, name)
}

// Source returns the projection governing source suppressions.
func (s *Scope) Source() *configuration.Config { return s.source }

// Composite returns the resolved invocation settings for script collection.
func (s *Scope) Composite() *configuration.Config { return s.composite }

// Caller returns the entry settings governing dependency blocking and path filters.
func (s *Scope) Caller() *configuration.Config { return s.caller }
