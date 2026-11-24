package component

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/runners/runnerapi"
	"github.com/kjanat/wacogo"
)

func (p *Plugin) HasRunnerProvider() bool { return p.runnerDescriptor != nil }
func (p *Plugin) RunnerDescriptor() runnerapi.Descriptor {
	if p.runnerDescriptor == nil {
		return runnerapi.Descriptor{}
	}
	d := *p.runnerDescriptor
	d.Targets = slices.Clone(d.Targets)
	return d
}

func (p *Plugin) loadRunner(ctx context.Context) error {
	value, err := p.callInterface(ctx, p.runner.interfaceName(), "describe")
	if err != nil {
		return err
	}
	descriptor, err := liftRunnerDescriptor(value)
	if err != nil {
		return err
	}
	p.runnerDescriptor = &descriptor
	return nil
}

func (p *Plugin) ResolveRunners(ctx context.Context, request runnerapi.Request) (runnerapi.Response, error) {
	var response runnerapi.Response
	if !p.HasRunnerProvider() {
		return response, errors.New("component does not implement the runner-provider role")
	}
	input, err := p.runner.lowerRequest(request)
	if err != nil {
		return response, err
	}
	value, err := p.callInterface(ctx, p.runner.interfaceName(), "resolve", input)
	if err != nil {
		return response, err
	}
	result, err := liftRecord(value, "runner outcome")
	if err != nil {
		return response, err
	}
	completion, ok := result.Field("error").(*wacogo.ValOption)
	if !ok {
		return response, liftError{"runner error", result.Field("error")}
	}
	if !completion.IsNone() {
		message, ok := completion.Val().(wacogo.ValString)
		if !ok {
			return response, liftError{"runner error message", completion.Val()}
		}
		return response, errors.New(string(message))
	}
	list, err := liftList(result.Field("answers"), "runner answers")
	if err != nil {
		return response, err
	}
	answers := make([]runnerapi.Answer, 0, list.Len())
	for i := range list.Len() {
		answer, err := liftRunnerAnswer(list.Get(i), p.runner)
		if err != nil {
			return response, err
		}
		answers = append(answers, answer)
	}
	response.Support, err = p.runner.liftSupport(result, request)
	if err != nil {
		return runnerapi.Response{}, err
	}
	response.Answers = answers
	return response, nil
}

func liftRunnerDescriptor(value wacogo.Val) (runnerapi.Descriptor, error) {
	r, err := liftRecord(value, "runner descriptor")
	if err != nil {
		return runnerapi.Descriptor{}, err
	}
	var d runnerapi.Descriptor
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

func lowerTerm(term runnerapi.Term) (wacogo.Val, error) {
	switch term := term.(type) {
	case runnerapi.Literal:
		return wacogo.NewValVariant(uint32(slices.Index(runner_term, "literal")), wacogo.ValString(term)), nil
	case runnerapi.Expression:
		return wacogo.NewValVariant(uint32(slices.Index(runner_term, "expression")), wacogo.ValString(term)), nil
	default:
		return nil, errors.New("invalid runner selector term")
	}
}

func lowerRunnerRequest(request runnerapi.Request) (wacogo.Val, error) {
	freshness := request.Snapshot.Freshness
	if freshness == "" {
		freshness = runnerapi.Missing
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

func lowerConstraint(c runnerapi.Constraint) (wacogo.Val, error) {
	value, err := lowerTerm(c.Value)
	if err != nil {
		return nil, err
	}
	return wacogo.NewValRecord(field("value", value), field("position", lowerPosition(c.Position))), nil
}

func liftRunnerAnswer(value wacogo.Val, protocol runnerProtocol) (runnerapi.Answer, error) {
	r, err := liftRecord(value, "runner answer")
	if err != nil {
		return runnerapi.Answer{}, err
	}
	var answer runnerapi.Answer
	answer.DatasetStatus, err = liftRunnerDataset(r.Field("dataset-status"))
	if err != nil {
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
		var candidates []runnerapi.Candidate
		for i := range list.Len() {
			c, err := liftRunnerCandidate(list.Get(i), protocol)
			if err != nil {
				return answer, err
			}
			candidates = append(candidates, c)
		}
		answer.Resolution = runnerapi.Resolved{Candidates: candidates}
	case "no-match":
		list, err := liftList(resolution.Val(), "runner rejections")
		if err != nil {
			return answer, err
		}
		var rejections []runnerapi.Rejection
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
			rejections = append(rejections, runnerapi.Rejection{Reason: reason, Position: pos})
		}
		answer.Resolution = runnerapi.NoMatch{Rejections: rejections}
	case "unknown":
		reason, ok := resolution.Val().(wacogo.ValString)
		if !ok {
			return answer, liftError{"runner reason", resolution.Val()}
		}
		answer.Resolution = runnerapi.Unknown{Reason: string(reason)}
	case "not-applicable":
		answer.Resolution = runnerapi.NotApplicable{}
	}
	return answer, nil
}

func liftRunnerPosition(value wacogo.Val) (sourcetext.Position, error) {
	// Runner rejections may address the containing job with this sentinel.
	if record, ok := value.(*wacogo.ValRecord); ok && record.Field("line") == wacogo.ValU32(0) && record.Field("column") == wacogo.ValU32(0) {
		return sourcetext.Position{}, nil
	}
	return liftPosition(value)
}

func liftRunnerCandidate(value wacogo.Val, protocol runnerProtocol) (runnerapi.Candidate, error) {
	r, err := liftRecord(value, "runner candidate")
	if err != nil {
		return runnerapi.Candidate{}, err
	}
	var c runnerapi.Candidate
	if c.Hosting, err = protocol.liftHosting(r); err != nil {
		return c, err
	}
	for name, dest := range map[string]*string{"name": &c.Name, "architecture": &c.Architecture, "default-shell": &c.DefaultShell} {
		if *dest, err = liftString(r, name); err != nil {
			return c, err
		}
	}
	os, err := liftRunnerEnum(r.Field("os"), runner_os)
	if err != nil {
		return c, err
	}
	c.OS = runnerapi.OS(os)
	caseSensitivity, err := liftRunnerEnum(r.Field("case-sensitivity"), runner_case_sensitivity)
	if err != nil {
		return c, err
	}
	c.CaseSensitivity = runnerapi.CaseSensitivity(caseSensitivity)
	for name, dest := range map[string]*bool{"sh-is-dash": &c.ShIsDash, "ephemeral": &c.Ephemeral} {
		if *dest, err = liftBool(r, name); err != nil {
			return c, err
		}
	}
	return c, nil
}
