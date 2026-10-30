package syntax

import (
	wl "actionlint.kjanat.dev/v2/internal/workflow"
)

// The functions below copy the parser's tree into the public value contract.
// Every node is freshly allocated, so the returned tree shares no storage with
// the parser. A nil input node converts to a nil output node.

func convertSlice[T, U any](in []T, convert func(T) U) []U {
	if in == nil {
		return nil
	}
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = convert(v)
	}
	return out
}

func convertMap[K comparable, T, U any](in map[K]T, convert func(T) U) map[K]U {
	if in == nil {
		return nil
	}
	out := make(map[K]U, len(in))
	for k, v := range in {
		out[k] = convert(v)
	}
	return out
}

func fromPos(p *wl.Pos) *Pos {
	if p == nil {
		return nil
	}
	return &Pos{Line: p.Line, Col: p.Col}
}

func fromString(s *wl.String) *String {
	if s == nil {
		return nil
	}
	return &String{Value: s.Value, Quoted: s.Quoted, Pos: fromPos(s.Pos)}
}

func fromStrings(in []*wl.String) []*String { return convertSlice(in, fromString) }

func fromBool(b *wl.Bool) *Bool {
	if b == nil {
		return nil
	}
	return &Bool{Value: b.Value, Expression: fromString(b.Expression), Pos: fromPos(b.Pos)}
}

func fromInt(i *wl.Int) *Int {
	if i == nil {
		return nil
	}
	return &Int{Value: i.Value, Expression: fromString(i.Expression), TruncatedFrom: i.TruncatedFrom, Pos: fromPos(i.Pos)}
}

func fromFloat(f *wl.Float) *Float {
	if f == nil {
		return nil
	}
	return &Float{Value: f.Value, Expression: fromString(f.Expression), Pos: fromPos(f.Pos)}
}

func fromFilter(f *wl.WebhookEventFilter) *WebhookEventFilter {
	if f == nil {
		return nil
	}
	return &WebhookEventFilter{Name: fromString(f.Name), Values: fromStrings(f.Values)}
}

func fromEvent(e wl.Event) Event {
	switch e := e.(type) {
	case *wl.WebhookEvent:
		if e == nil {
			return (*WebhookEvent)(nil)
		}
		return &WebhookEvent{
			Hook: fromString(e.Hook), Types: fromStrings(e.Types),
			Branches: fromFilter(e.Branches), BranchesIgnore: fromFilter(e.BranchesIgnore),
			Tags: fromFilter(e.Tags), TagsIgnore: fromFilter(e.TagsIgnore),
			Paths: fromFilter(e.Paths), PathsIgnore: fromFilter(e.PathsIgnore),
			Workflows: fromStrings(e.Workflows), Pos: fromPos(e.Pos),
		}
	case *wl.ScheduledEvent:
		if e == nil {
			return (*ScheduledEvent)(nil)
		}
		return &ScheduledEvent{Schedules: convertSlice(e.Schedules, fromScheduleEntry), Pos: fromPos(e.Pos)}
	case *wl.WorkflowDispatchEvent:
		if e == nil {
			return (*WorkflowDispatchEvent)(nil)
		}
		return &WorkflowDispatchEvent{Inputs: convertMap(e.Inputs, fromDispatchInput), Pos: fromPos(e.Pos)}
	case *wl.RepositoryDispatchEvent:
		if e == nil {
			return (*RepositoryDispatchEvent)(nil)
		}
		return &RepositoryDispatchEvent{Types: fromStrings(e.Types), Pos: fromPos(e.Pos)}
	case *wl.WorkflowCallEvent:
		if e == nil {
			return (*WorkflowCallEvent)(nil)
		}
		return &WorkflowCallEvent{
			Inputs:  convertSlice(e.Inputs, fromWorkflowCallEventInput),
			Secrets: convertMap(e.Secrets, fromWorkflowCallEventSecret),
			Outputs: convertMap(e.Outputs, fromWorkflowCallEventOutput),
			Pos:     fromPos(e.Pos),
		}
	case *wl.ImageVersionEvent:
		if e == nil {
			return (*ImageVersionEvent)(nil)
		}
		return &ImageVersionEvent{Types: fromStrings(e.Types), Names: fromStrings(e.Names), Versions: fromStrings(e.Versions), Pos: fromPos(e.Pos)}
	}
	// The parser produces only the event types above.
	return nil
}

func fromScheduleEntry(e *wl.ScheduleEntry) *ScheduleEntry {
	if e == nil {
		return nil
	}
	return &ScheduleEntry{Cron: fromString(e.Cron), Timezone: fromString(e.Timezone)}
}

func fromDispatchInput(i *wl.DispatchInput) *DispatchInput {
	if i == nil {
		return nil
	}
	return &DispatchInput{
		Name: fromString(i.Name), Description: fromString(i.Description), Required: fromBool(i.Required),
		Default: fromString(i.Default), Type: WorkflowDispatchEventInputType(i.Type), Options: fromStrings(i.Options),
	}
}

func fromWorkflowCallEventInput(i *wl.WorkflowCallEventInput) *WorkflowCallEventInput {
	if i == nil {
		return nil
	}
	return &WorkflowCallEventInput{
		Name: fromString(i.Name), Description: fromString(i.Description), Default: fromString(i.Default),
		Required: fromBool(i.Required), Type: WorkflowCallEventInputType(i.Type), ID: i.ID,
	}
}

func fromWorkflowCallEventSecret(s *wl.WorkflowCallEventSecret) *WorkflowCallEventSecret {
	if s == nil {
		return nil
	}
	return &WorkflowCallEventSecret{Name: fromString(s.Name), Description: fromString(s.Description), Required: fromBool(s.Required)}
}

func fromWorkflowCallEventOutput(o *wl.WorkflowCallEventOutput) *WorkflowCallEventOutput {
	if o == nil {
		return nil
	}
	return &WorkflowCallEventOutput{Name: fromString(o.Name), Description: fromString(o.Description), Value: fromString(o.Value)}
}

func fromPermissions(p *wl.Permissions) *Permissions {
	if p == nil {
		return nil
	}
	return &Permissions{
		All: fromString(p.All),
		Scopes: convertMap(p.Scopes, func(s *wl.PermissionScope) *PermissionScope {
			if s == nil {
				return nil
			}
			return &PermissionScope{Name: fromString(s.Name), Value: fromString(s.Value)}
		}),
		Pos: fromPos(p.Pos),
	}
}

func fromCacheMode(m *wl.CacheMode) *CacheMode {
	if m == nil {
		return nil
	}
	return &CacheMode{Kind: CacheModeKind(m.Kind), Pos: fromPos(m.Pos)}
}

func fromDefaults(d *wl.Defaults) *Defaults {
	if d == nil {
		return nil
	}
	out := &Defaults{Pos: fromPos(d.Pos)}
	if r := d.Run; r != nil {
		out.Run = &DefaultsRun{Expression: fromString(r.Expression), Shell: fromString(r.Shell), WorkingDirectory: fromString(r.WorkingDirectory), Pos: fromPos(r.Pos)}
	}
	return out
}

func fromConcurrency(c *wl.Concurrency) *Concurrency {
	if c == nil {
		return nil
	}
	return &Concurrency{
		Expression: fromString(c.Expression), Group: fromString(c.Group), CancelInProgress: fromBool(c.CancelInProgress),
		Queue: fromString(c.Queue), Pos: fromPos(c.Pos),
	}
}

func fromEnvironment(e *wl.Environment) *Environment {
	if e == nil {
		return nil
	}
	return &Environment{
		Expression: fromString(e.Expression), Name: fromString(e.Name), URL: fromString(e.URL),
		Deployment: fromBool(e.Deployment), Pos: fromPos(e.Pos),
	}
}

func fromInput(i *wl.Input) *Input {
	if i == nil {
		return nil
	}
	return &Input{Name: fromString(i.Name), Value: fromString(i.Value)}
}

func fromActionInputMapping(m *wl.ActionInputMapping) *ActionInputMapping {
	if m == nil {
		return nil
	}
	return &ActionInputMapping{
		Inputs: convertMap(m.Inputs, fromInput), Expression: fromString(m.Expression),
		Insertions: convertSlice(m.Insertions, fromActionInputMapping),
	}
}

func fromExec(e wl.Exec) Exec {
	switch e := e.(type) {
	case *wl.ExecRun:
		if e == nil {
			return (*ExecRun)(nil)
		}
		return &ExecRun{Run: fromString(e.Run), Shell: fromString(e.Shell), WorkingDirectory: fromString(e.WorkingDirectory), RunPos: fromPos(e.RunPos)}
	case *wl.ExecAction:
		if e == nil {
			return (*ExecAction)(nil)
		}
		return &ExecAction{
			Uses: fromString(e.Uses), Inputs: convertMap(e.Inputs, fromInput), InputsExpression: fromString(e.InputsExpression),
			InputInsertions: convertSlice(e.InputInsertions, fromActionInputMapping),
			Entrypoint:      fromString(e.Entrypoint), Args: fromString(e.Args),
			EntrypointEvaluated: e.EntrypointEvaluated, ArgsEvaluated: e.ArgsEvaluated,
			InputNameExpressions: fromStrings(e.InputNameExpressions),
		}
	case *wl.ExecWait:
		if e == nil {
			return (*ExecWait)(nil)
		}
		return &ExecWait{Names: fromStrings(e.Names), All: e.All, AllPos: fromPos(e.AllPos)}
	case *wl.ExecCancel:
		if e == nil {
			return (*ExecCancel)(nil)
		}
		return &ExecCancel{Name: fromString(e.Name)}
	case *wl.ExecParallel:
		if e == nil {
			return (*ExecParallel)(nil)
		}
		return &ExecParallel{Steps: convertSlice(e.Steps, fromStep)}
	}
	// The parser produces only the execution types above.
	return nil
}

func fromRawYAML(v wl.RawYAMLValue) RawYAMLValue {
	switch v := v.(type) {
	case *wl.RawYAMLObject:
		if v == nil {
			return (*RawYAMLObject)(nil)
		}
		return &RawYAMLObject{Props: convertMap(v.Props, fromRawYAML), Position: fromPos(v.Position)}
	case *wl.RawYAMLArray:
		if v == nil {
			return (*RawYAMLArray)(nil)
		}
		return &RawYAMLArray{Elems: convertSlice(v.Elems, fromRawYAML), Position: fromPos(v.Position)}
	case *wl.RawYAMLString:
		if v == nil {
			return (*RawYAMLString)(nil)
		}
		return &RawYAMLString{Value: v.Value, Tag: v.Tag, Position: fromPos(v.Position)}
	}
	// The parser produces only the raw value types above.
	return nil
}

func fromMatrixCombinations(cs *wl.MatrixCombinations) *MatrixCombinations {
	if cs == nil {
		return nil
	}
	return &MatrixCombinations{
		Name: fromString(cs.Name), Evaluated: cs.Evaluated, Expression: fromString(cs.Expression),
		Combinations: convertSlice(cs.Combinations, func(c *wl.MatrixCombination) *MatrixCombination {
			if c == nil {
				return nil
			}
			return &MatrixCombination{
				Evaluated: c.Evaluated, Expression: fromString(c.Expression),
				Assigns: convertMap(c.Assigns, func(a *wl.MatrixAssign) *MatrixAssign {
					if a == nil {
						return nil
					}
					return &MatrixAssign{Key: fromString(a.Key), Value: fromRawYAML(a.Value)}
				}),
			}
		}),
	}
}

func fromMatrix(m *wl.Matrix) *Matrix {
	if m == nil {
		return nil
	}
	return &Matrix{
		Insertions: convertSlice(m.Insertions, fromMatrix),
		Rows: convertMap(m.Rows, func(r *wl.MatrixRow) *MatrixRow {
			if r == nil {
				return nil
			}
			return &MatrixRow{Evaluated: r.Evaluated, Name: fromString(r.Name), Values: convertSlice(r.Values, fromRawYAML), Expression: fromString(r.Expression)}
		}),
		Include: fromMatrixCombinations(m.Include), Exclude: fromMatrixCombinations(m.Exclude),
		Expression: fromString(m.Expression), Pos: fromPos(m.Pos),
	}
}

func fromStrategy(s *wl.Strategy) *Strategy {
	if s == nil {
		return nil
	}
	return &Strategy{
		Expression: fromString(s.Expression), Matrix: fromMatrix(s.Matrix), FailFast: fromBool(s.FailFast),
		MaxParallel: fromInt(s.MaxParallel), Pos: fromPos(s.Pos),
	}
}

func fromEnv(e *wl.Env) *Env {
	if e == nil {
		return nil
	}
	return &Env{
		Vars: convertMap(e.Vars, func(v *wl.EnvVar) *EnvVar {
			if v == nil {
				return nil
			}
			return &EnvVar{Name: fromString(v.Name), Value: fromString(v.Value)}
		}),
		Insertions: convertSlice(e.Insertions, fromEnv),
		Expression: fromString(e.Expression),
	}
}

func fromStep(s *wl.Step) *Step {
	if s == nil {
		return nil
	}
	return &Step{
		ID: fromString(s.ID), If: fromString(s.If), Name: fromString(s.Name), Exec: fromExec(s.Exec),
		DeclaredUses: fromString(s.DeclaredUses), Env: fromEnv(s.Env), ContinueOnError: fromBool(s.ContinueOnError),
		TimeoutMinutes: fromFloat(s.TimeoutMinutes), Background: fromBool(s.Background), Pos: fromPos(s.Pos),
	}
}

func fromContainer(c *wl.Container) *Container {
	if c == nil {
		return nil
	}
	out := &Container{
		Expression: fromString(c.Expression), Image: fromString(c.Image), Env: fromEnv(c.Env),
		Ports: fromStrings(c.Ports), PortsExpression: fromString(c.PortsExpression),
		Volumes: fromStrings(c.Volumes), VolumesExpression: fromString(c.VolumesExpression),
		Options: fromString(c.Options), Command: fromString(c.Command), Entrypoint: fromString(c.Entrypoint),
		Pos: fromPos(c.Pos),
	}
	if cr := c.Credentials; cr != nil {
		out.Credentials = &Credentials{Username: fromString(cr.Username), Password: fromString(cr.Password), Expression: fromString(cr.Expression), Pos: fromPos(cr.Pos)}
	}
	return out
}

func fromServices(s *wl.Services) *Services {
	if s == nil {
		return nil
	}
	return &Services{
		Value: convertMap(s.Value, func(v *wl.Service) *Service {
			if v == nil {
				return nil
			}
			return &Service{Name: fromString(v.Name), Container: fromContainer(v.Container)}
		}),
		Expression: fromString(s.Expression), Pos: fromPos(s.Pos),
	}
}

func fromRunner(r *wl.Runner) *Runner {
	if r == nil {
		return nil
	}
	return &Runner{Expression: fromString(r.Expression), Labels: fromStrings(r.Labels), LabelsExpr: fromString(r.LabelsExpr), Group: fromString(r.Group)}
}

func fromWorkflowCall(c *wl.WorkflowCall) *WorkflowCall {
	if c == nil {
		return nil
	}
	return &WorkflowCall{
		Uses: fromString(c.Uses),
		Inputs: convertMap(c.Inputs, func(i *wl.WorkflowCallInput) *WorkflowCallInput {
			if i == nil {
				return nil
			}
			return &WorkflowCallInput{Name: fromString(i.Name), Value: fromString(i.Value)}
		}),
		Secrets: convertMap(c.Secrets, func(s *wl.WorkflowCallSecret) *WorkflowCallSecret {
			if s == nil {
				return nil
			}
			return &WorkflowCallSecret{Name: fromString(s.Name), Value: fromString(s.Value)}
		}),
		InheritSecrets: c.InheritSecrets,
	}
}

func fromSnapshot(s *wl.Snapshot) *Snapshot {
	if s == nil {
		return nil
	}
	return &Snapshot{Expression: fromString(s.Expression), ImageName: fromString(s.ImageName), Version: fromString(s.Version), If: fromString(s.If)}
}

func fromJob(j *wl.Job) *Job {
	if j == nil {
		return nil
	}
	return &Job{
		ID: fromString(j.ID), Name: fromString(j.Name), Needs: fromStrings(j.Needs), RunsOn: fromRunner(j.RunsOn),
		Permissions: fromPermissions(j.Permissions), CacheMode: fromCacheMode(j.CacheMode),
		Environment: fromEnvironment(j.Environment), Concurrency: fromConcurrency(j.Concurrency),
		Outputs: convertMap(j.Outputs, func(o *wl.Output) *Output {
			if o == nil {
				return nil
			}
			return &Output{Name: fromString(o.Name), Value: fromString(o.Value)}
		}),
		Env: fromEnv(j.Env), Defaults: fromDefaults(j.Defaults), If: fromString(j.If),
		Steps: convertSlice(j.Steps, fromStep), TimeoutMinutes: fromFloat(j.TimeoutMinutes),
		CancelTimeoutMinutes: fromFloat(j.CancelTimeoutMinutes), Strategy: fromStrategy(j.Strategy),
		ContinueOnError: fromBool(j.ContinueOnError), Container: fromContainer(j.Container),
		Services: fromServices(j.Services), WorkflowCall: fromWorkflowCall(j.WorkflowCall),
		DeclaredUses: fromString(j.DeclaredUses), Snapshot: fromSnapshot(j.Snapshot), Pos: fromPos(j.Pos),
	}
}

func fromWorkflow(w *wl.Workflow) *Workflow {
	if w == nil {
		return nil
	}
	return &Workflow{
		Name: fromString(w.Name), Description: fromString(w.Description), RunName: fromString(w.RunName),
		On: convertSlice(w.On, fromEvent), OnPos: fromPos(w.OnPos), Permissions: fromPermissions(w.Permissions),
		CacheMode: fromCacheMode(w.CacheMode), Env: fromEnv(w.Env), Defaults: fromDefaults(w.Defaults),
		Concurrency: fromConcurrency(w.Concurrency), Jobs: convertMap(w.Jobs, fromJob), Pos: fromPos(w.Pos),
	}
}
