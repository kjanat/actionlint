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

type Rule struct {
	scope      *Scope
	id         binding.ID
	execution  *configuration.Config
	policy     *configuration.Config
	options    json.RawMessage
	optionsErr error
}

func (r *Rule) Options(explicit json.RawMessage) (json.RawMessage, error) {
	if len(explicit) != 0 {
		return slices.Clone(explicit), nil
	}
	return slices.Clone(r.options), r.optionsErr
}

// ID is zero when the rule is unbound.
func (r *Rule) ID() binding.ID { return r.id }

type Registry struct {
	rules     configuration.RuleSet
	records   []*Rule
	namespace binding.Namespace
	sealed    bool
}

type SealedRegistry struct {
	namespace binding.Namespace
	records   []*Rule
}

func (r *Registry) Seal() *SealedRegistry {
	r.sealed = true
	return &SealedRegistry{namespace: r.namespace, records: r.records}
}

func NewRegistry(rules configuration.RuleSet) *Registry {
	return &Registry{rules: rules, namespace: binding.NewNamespace()}
}

func (r *Registry) add(execution, policy *configuration.Config) *Rule {
	decision := &Rule{id: r.namespace.ID(uint64(len(r.records)) + 1), execution: execution, policy: policy}
	r.records = append(r.records, decision)
	return decision
}

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

// Config returns settings that consumers must not mutate.
func (r *Rule) Config() *configuration.Config { return r.execution }

func (r *Rule) Policy() *configuration.Config { return r.policy }

func (r *Rule) Scope() *Scope { return r.scope }

type Scope struct {
	rules     configuration.RuleSet
	bindings  map[string]*Rule
	fallback  *Rule
	source    *configuration.Config
	composite *configuration.Config
	caller    *configuration.Config
}

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

// Rule returns the prepared caller binding for undeclared diagnostics.
func (s *Scope) Rule(name string) *Rule {
	if r, exists := s.bindings[s.rules.ResolveAlias(name)]; exists {
		return r
	}
	return s.fallback
}

func (s *Scope) DiagnosticLevel(name string) ruleconfig.Level {
	return s.rules.DiagnosticLevel(s.Rule(name).policy, name)
}

func (s *Scope) Source() *configuration.Config { return s.source }

func (s *Scope) Composite() *configuration.Config { return s.composite }

func (s *Scope) Caller() *configuration.Config { return s.caller }
