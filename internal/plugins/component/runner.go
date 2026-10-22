package component

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/diagnostic"
	"actionlint.kjanat.dev/runner"
	"github.com/partite-ai/wacogo"
)

func (p *Plugin) HasAnalyzer() bool       { return p.analyzerRole }
func (p *Plugin) HasRunnerProvider() bool { return p.runnerRole }
func (p *Plugin) ArtifactDigest() string  { return p.digest }
func (p *Plugin) RunnerDescriptor() runner.Descriptor {
	d := p.runnerDescriptor
	d.Targets = slices.Clone(d.Targets)
	return d
}

func (p *Plugin) ResolveRunners(ctx context.Context, request runner.Request) ([]runner.Answer, error) {
	if !p.runnerRole {
		return nil, errors.New("component does not implement the runner-provider role")
	}
	input, err := lowerRunnerRequest(request)
	if err != nil {
		return nil, err
	}
	values, err := p.callInterface(ctx, RunnerInterface, "resolve", input)
	if err != nil {
		return nil, err
	}
	result, err := liftRecord(values[0], "runner outcome")
	if err != nil {
		return nil, err
	}
	completion, ok := result.Field("error").(*wacogo.ValOption)
	if !ok {
		return nil, liftError{"runner error", result.Field("error")}
	}
	if !completion.IsNone() {
		message, ok := completion.Val().(wacogo.ValString)
		if !ok {
			return nil, liftError{"runner error message", completion.Val()}
		}
		return nil, errors.New(string(message))
	}
	list, err := liftList(result.Field("answers"), "runner answers")
	if err != nil {
		return nil, err
	}
	answers := make([]runner.Answer, 0, list.Len())
	for i := range list.Len() {
		answer, err := liftRunnerAnswer(list.Get(i))
		if err != nil {
			return nil, err
		}
		answers = append(answers, answer)
	}
	return answers, nil
}

func liftRunnerDescriptor(value wacogo.Val) (runner.Descriptor, error) {
	r, err := liftRecord(value, "runner descriptor")
	if err != nil {
		return runner.Descriptor{}, err
	}
	var d runner.Descriptor
	if d.Name, err = liftString(r, "name"); err != nil {
		return d, err
	}
	if d.Version, err = liftString(r, "version"); err != nil {
		return d, err
	}
	list, err := liftList(r.Field("targets"), "runner targets")
	if err != nil {
		return d, err
	}
	for i := range list.Len() {
		value, ok := list.Get(i).(wacogo.ValString)
		if !ok {
			return d, liftError{"runner target", list.Get(i)}
		}
		if value == "" || slices.Contains(d.Targets, string(value)) {
			return d, errors.New("empty or duplicate runner target")
		}
		d.Targets = append(d.Targets, string(value))
	}
	if d.Name == "" || d.Version == "" || len(d.Targets) == 0 {
		return d, errors.New("runner descriptor needs a name, version and targets")
	}
	return d, nil
}

func runnerEnum(names []string, name string) (wacogo.Val, error) {
	index := slices.Index(names, name)
	if index < 0 {
		return nil, fmt.Errorf("unknown runner enum %q", name)
	}
	return wacogo.NewValEnum(uint32(index)), nil
}

func lowerTerm(term runner.Term) (wacogo.Val, error) {
	switch term := term.(type) {
	case runner.Literal:
		return wacogo.NewValVariant(uint32(slices.Index(runner_term, "literal")), wacogo.ValString(term)), nil
	case runner.Expression:
		return wacogo.NewValVariant(uint32(slices.Index(runner_term, "expression")), wacogo.ValString(term)), nil
	default:
		return nil, errors.New("invalid runner selector term")
	}
}

func lowerRunnerRequest(request runner.Request) (wacogo.Val, error) {
	freshness := request.Snapshot.Freshness
	if freshness == "" {
		freshness = runner.Missing
	}
	fresh, err := runnerEnum(runner_freshness, string(freshness))
	if err != nil {
		return nil, err
	}
	var selectors []wacogo.Val
	for _, selector := range request.Selectors {
		var labels []wacogo.Val
		for _, label := range selector.Labels {
			value, err := lowerConstraint(label)
			if err != nil {
				return nil, err
			}
			labels = append(labels, value)
		}
		group := wacogo.ValOptionNone()
		if selector.Group != nil {
			value, err := lowerConstraint(*selector.Group)
			if err != nil {
				return nil, err
			}
			group = wacogo.ValOptionSome(value)
		}
		selectors = append(selectors, wacogo.NewValRecord(field("incomplete", wacogo.ValBool(selector.Incomplete)), field("labels", wacogo.NewValListOf(labels...)), field("group", group), field("expression", wacogo.ValString(selector.Expression))))
	}
	s := request.Snapshot
	snapshot := wacogo.NewValRecord(field("content", wacogo.ValString(s.Content)), field("scope", wacogo.ValString(s.Scope)), field("obtained-at", wacogo.ValString(s.ObtainedAt)), field("freshness", fresh), field("complete", wacogo.ValBool(s.Complete)))
	return wacogo.NewValRecord(field("target", wacogo.ValString(request.Target)), field("scope", wacogo.ValString(request.Scope)), field("snapshot", snapshot), field("selectors", wacogo.NewValListOf(selectors...))), nil
}

func liftRunnerEnum(value wacogo.Val, names []string) (string, error) {
	enum, ok := value.(*wacogo.ValEnum)
	if !ok || int(enum.Discriminant()) >= len(names) {
		return "", liftError{"runner enum", value}
	}
	return names[enum.Discriminant()], nil
}

func liftBool(r *wacogo.ValRecord, name string) (bool, error) {
	value, ok := r.Field(name).(wacogo.ValBool)
	if !ok {
		return false, liftError{name, r.Field(name)}
	}
	return bool(value), nil
}

func lowerConstraint(c runner.Constraint) (wacogo.Val, error) {
	value, err := lowerTerm(c.Value)
	if err != nil {
		return nil, err
	}
	return wacogo.NewValRecord(field("value", value), field("position", lowerPosition(c.Position))), nil
}

func liftRunnerAnswer(value wacogo.Val) (runner.Answer, error) {
	r, err := liftRecord(value, "runner answer")
	if err != nil {
		return runner.Answer{}, err
	}
	var answer runner.Answer
	e, err := liftRecord(r.Field("evidence"), "runner evidence")
	if err != nil {
		return answer, err
	}
	for name, dest := range map[string]*string{"dataset": &answer.Evidence.Dataset, "scope": &answer.Evidence.Scope, "obtained-at": &answer.Evidence.ObtainedAt} {
		if *dest, err = liftString(e, name); err != nil {
			return answer, err
		}
	}
	fresh, err := liftRunnerEnum(e.Field("freshness"), runner_freshness)
	if err != nil {
		return answer, err
	}
	answer.Evidence.Freshness = runner.Freshness(fresh)
	if answer.Evidence.Complete, err = liftBool(e, "complete"); err != nil {
		return answer, err
	}
	resolution, ok := r.Field("resolution").(*wacogo.ValVariant)
	if !ok || int(resolution.Discriminant()) >= len(runner_resolution) {
		return answer, liftError{"runner resolution", r.Field("resolution")}
	}
	switch runner_resolution[resolution.Discriminant()] {
	case "resolved":
		list, err := liftList(resolution.Val(), "runner candidates")
		if err != nil {
			return answer, err
		}
		var candidates []runner.Candidate
		for i := range list.Len() {
			c, err := liftRunnerCandidate(list.Get(i))
			if err != nil {
				return answer, err
			}
			candidates = append(candidates, c)
		}
		answer.Resolution = runner.Resolved{Candidates: candidates}
	case "no-match":
		list, err := liftList(resolution.Val(), "runner rejections")
		if err != nil {
			return answer, err
		}
		var rejections []runner.Rejection
		for i := range list.Len() {
			r, err := liftRecord(list.Get(i), "runner rejection")
			if err != nil {
				return answer, err
			}
			reason, err := liftString(r, "reason")
			if err != nil {
				return answer, err
			}
			pos, err := liftRunnerPosition(r.Field("position"))
			if err != nil {
				return answer, err
			}
			rejections = append(rejections, runner.Rejection{Reason: reason, Position: pos})
		}
		answer.Resolution = runner.NoMatch{Rejections: rejections}
	case "unknown":
		reason, ok := resolution.Val().(wacogo.ValString)
		if !ok {
			return answer, liftError{"runner reason", resolution.Val()}
		}
		answer.Resolution = runner.Unknown{Reason: string(reason)}
	case "not-applicable":
		answer.Resolution = runner.NotApplicable{}
	}
	return answer, nil
}

func liftRunnerPosition(value wacogo.Val) (diagnostic.Position, error) {
	// Runner rejections may address the containing job. This sentinel is not
	// valid for analyzer diagnostics; all other coordinates use their checks.
	if record, ok := value.(*wacogo.ValRecord); ok && record.Field("line") == wacogo.ValU32(0) && record.Field("column") == wacogo.ValU32(0) {
		return diagnostic.Position{}, nil
	}
	return liftPosition(value)
}

func liftRunnerCandidate(value wacogo.Val) (runner.Candidate, error) {
	r, err := liftRecord(value, "runner candidate")
	if err != nil {
		return runner.Candidate{}, err
	}
	var c runner.Candidate
	for name, dest := range map[string]*string{"name": &c.Name, "architecture": &c.Architecture, "default-shell": &c.DefaultShell} {
		if *dest, err = liftString(r, name); err != nil {
			return c, err
		}
	}
	os, err := liftRunnerEnum(r.Field("os"), runner_os)
	if err != nil {
		return c, err
	}
	c.OS = runner.OS(os)
	caseSensitivity, err := liftRunnerEnum(r.Field("case-sensitivity"), runner_case_sensitivity)
	if err != nil {
		return c, err
	}
	c.CaseSensitivity = runner.CaseSensitivity(caseSensitivity)
	for name, dest := range map[string]*bool{"sh-is-dash": &c.ShIsDash, "ephemeral": &c.Ephemeral} {
		if *dest, err = liftBool(r, name); err != nil {
			return c, err
		}
	}
	return c, nil
}
