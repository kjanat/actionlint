package component

import (
	"errors"
	"fmt"

	runner "actionlint.kjanat.dev/v2/internal/model/runners/contract"
	"actionlint.kjanat.dev/v2/internal/model/services"
	"github.com/kjanat/wacogo"
)

func lowerRunner03Request(request runner.Request, common wacogo.Val) (wacogo.Val, error) {
	if request.AsOf.IsZero() {
		return nil, errors.New("runner support assessment requires a date")
	}
	r, err := liftRecord(common, "runner request")
	if err != nil {
		return nil, err
	}
	version := wacogo.ValOptionNone()
	if request.Service.Version() != "" {
		version = wacogo.ValOptionSome(wacogo.ValString(request.Service.Version()))
	}
	service := wacogo.NewValRecord(field("kind", wacogo.ValString(request.Service.Kind())), field("version", version))
	return wacogo.NewValRecord(
		field("target", r.Field("target")), field("service", service), field("as-of", wacogo.ValString(request.AsOf.String())),
		field("scope", r.Field("scope")), field("snapshot", r.Field("snapshot")), field("selectors", r.Field("selectors")),
	), nil
}

func liftRunnerHosting(value wacogo.Val) (runner.Hosting, error) {
	name, err := liftRunnerEnum(value, runner_03_hosting)
	if err != nil {
		return runner.UnknownHosting, err
	}
	if name == "unknown" {
		return runner.UnknownHosting, nil
	}
	return runner.Hosting(name), nil
}

func liftRunnerSupportState(value wacogo.Val) (runner.SupportState, error) {
	name, err := liftRunnerEnum(value, runner_03_support_state)
	if err != nil {
		return runner.SupportUnknown, err
	}
	if name == "unknown" {
		return runner.SupportUnknown, nil
	}
	return runner.SupportState(name), nil
}

func liftRunnerDataset(value wacogo.Val) (runner.DatasetStatus, error) {
	r, err := liftRecord(value, "runner dataset status")
	if err != nil {
		return runner.DatasetStatus{}, err
	}
	var status runner.DatasetStatus
	for name, dest := range map[string]*string{"dataset-id": &status.DatasetID, "scope": &status.Scope, "obtained-at": &status.ObtainedAt} {
		if *dest, err = liftString(r, name); err != nil {
			return status, err
		}
	}
	freshness, err := liftRunnerEnum(r.Field("freshness"), runner_freshness)
	if err != nil {
		return status, err
	}
	status.Freshness = runner.Freshness(freshness)
	status.Complete, err = liftBool(r, "complete")
	return status, err
}

func liftRunnerSupport(value wacogo.Val) (runner.SupportCatalog, error) {
	var catalog runner.SupportCatalog
	option, ok := value.(*wacogo.ValOption)
	if !ok {
		return catalog, liftError{"runner support catalogue", value}
	}
	if option.IsNone() {
		return catalog, nil
	}
	r, err := liftRecord(option.Val(), "runner support catalogue")
	if err != nil {
		return catalog, err
	}
	catalog.DatasetStatus, err = liftRunnerDataset(r.Field("dataset-status"))
	if err != nil {
		return catalog, err
	}
	list, err := liftList(r.Field("policies"), "runner support policies")
	if err != nil {
		return catalog, err
	}
	for i := range list.Len() {
		policy, err := liftRunnerSupportPolicy(list.Get(i))
		if err != nil {
			return catalog, fmt.Errorf("runner support policy %d: %w", i, err)
		}
		catalog.Policies = append(catalog.Policies, policy)
	}
	return catalog, nil
}

func liftRunnerSupportPolicy(value wacogo.Val) (runner.SupportPolicy, error) {
	var policy runner.SupportPolicy
	r, err := liftRecord(value, "runner support policy")
	if err != nil {
		return policy, err
	}
	for name, dest := range map[string]*string{"id": &policy.ID, "description": &policy.Description, "url": &policy.URL} {
		if *dest, err = liftString(r, name); err != nil {
			return policy, err
		}
	}
	capability, err := liftString(r, "capability")
	if err != nil {
		return policy, err
	}
	policy.Capability = runner.Capability(capability)
	service, err := liftRecord(r.Field("service"), "runner support service scope")
	if err != nil {
		return policy, err
	}
	kind, err := liftString(service, "kind")
	if err != nil {
		return policy, err
	}
	policy.Service.Kind = services.Kind(kind)
	for name, dest := range map[string]*string{"min-version": &policy.Service.MinVersion, "before-version": &policy.Service.BeforeVersion} {
		value, err := liftScriptOptionalString(service.Field(name), name)
		if err != nil {
			return policy, err
		}
		if value != nil {
			*dest = *value
		}
	}
	hosting, err := liftList(r.Field("hosting"), "runner support hosting")
	if err != nil {
		return policy, err
	}
	for i := range hosting.Len() {
		value, err := liftRunnerHosting(hosting.Get(i))
		if err != nil {
			return policy, err
		}
		policy.Hosting = append(policy.Hosting, value)
	}
	systems, err := liftList(r.Field("os"), "runner support operating systems")
	if err != nil {
		return policy, err
	}
	for i := range systems.Len() {
		value, err := liftRunnerEnum(systems.Get(i), runner_os)
		if err != nil {
			return policy, err
		}
		policy.OS = append(policy.OS, runner.OS(value))
	}
	architectures, err := liftList(r.Field("architectures"), "runner support architectures")
	if err != nil {
		return policy, err
	}
	for i := range architectures.Len() {
		value, ok := architectures.Get(i).(wacogo.ValString)
		if !ok {
			return policy, liftError{"runner support architecture", architectures.Get(i)}
		}
		policy.Architectures = append(policy.Architectures, string(value))
	}
	policy.Initial, err = liftRunnerSupportState(r.Field("initial"))
	if err != nil {
		return policy, err
	}
	changes, err := liftList(r.Field("changes"), "runner support changes")
	if err != nil {
		return policy, err
	}
	for i := range changes.Len() {
		change, err := liftRunnerSupportChange(changes.Get(i))
		if err != nil {
			return policy, err
		}
		policy.Changes = append(policy.Changes, change)
	}
	return policy, nil
}

func liftRunnerSupportChange(value wacogo.Val) (runner.SupportChange, error) {
	var change runner.SupportChange
	r, err := liftRecord(value, "runner support change")
	if err != nil {
		return change, err
	}
	date, err := liftString(r, "on")
	if err != nil {
		return change, err
	}
	change.On, err = runner.ParseDate(date)
	if err != nil {
		return change, err
	}
	change.State, err = liftRunnerSupportState(r.Field("state"))
	return change, err
}
