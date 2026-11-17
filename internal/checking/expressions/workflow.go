package expressions

import (
	"actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"
	"actionlint.kjanat.dev/v2/internal/model/contexts/outputs"

	"actionlint.kjanat.dev/v2/internal/model/contexts/events"

	"actionlint.kjanat.dev/v2/internal/model/workflows"

	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/schema"
)

func (rule *Checker) VisitWorkflowPre(n workflows.BoundWorkflow) error {
	rule.workflow = n
	rule.eventTy = new(events.Type(n))
	rule.checkString(n.Name, "")
	rule.checkString(n.Description, "")
	triggerCount := 0
	for range n.On {
		triggerCount++
	}
	hasNonWorkflowCallTrigger := triggerCount > 1
	for _, event := range n.On {
		if e, ok := event.(*definition.WebhookEvent); ok {
			rule.checkStrings(e.Types, "")
			for _, filter := range []*definition.WebhookEventFilter{e.Branches, e.BranchesIgnore, e.Tags, e.TagsIgnore, e.Paths, e.PathsIgnore} {
				rule.checkWebhookEventFilter(filter)
			}
			rule.checkStrings(e.Workflows, "")
		} else if e, ok := event.(*definition.ScheduledEvent); ok {
			for _, schedule := range e.Schedules {
				rule.checkString(schedule.Cron, "")
				rule.checkString(schedule.Timezone, "")
			}
		} else if e, ok := event.(*definition.WorkflowDispatchEvent); ok {
			var inputs exprtypes.ObjectBuilder
			for id, input := range e.Inputs {
				rule.checkString(input.Description, "")
				rule.checkString(input.Default, "")
				rule.checkBool(input.Required, "")
				rule.checkStrings(input.Options, "")
				ty := exprtypes.AnyType()
				switch input.Type {
				case definition.WorkflowDispatchEventInputTypeBoolean:
					ty = exprtypes.BoolType()
				case definition.WorkflowDispatchEventInputTypeNumber:
					ty = exprtypes.NumberType()
				case definition.WorkflowDispatchEventInputTypeString, definition.WorkflowDispatchEventInputTypeChoice, definition.WorkflowDispatchEventInputTypeEnvironment:
					ty = exprtypes.StringType()
				case definition.WorkflowDispatchEventInputTypeNone:
				}
				name := id
				if input.Name != nil {
					name = input.Name.Text()
				}
				inputs.SetProperty(name, ty)
			}
			rule.dispatchInputsTy = new(inputs.Build())
		} else if e, ok := event.(*definition.RepositoryDispatchEvent); ok {
			rule.checkStrings(e.Types, "")
		} else if e, ok := event.(*definition.WorkflowCallEvent); ok {
			var inputs exprtypes.ObjectBuilder
			// Defaults can use earlier inputs, but cannot refer to their own
			// definition, so rule.inputsTy is rebuilt after each input is added.
			rule.inputsTy = new(inputs.Build())
			for _, input := range e.Inputs {
				rule.checkString(input.Description, "")
				expressions := rule.checkString(input.Default, "on.workflow_call.inputs.<inputs_id>.default")
				ty := exprtypes.AnyType()
				switch input.Type {
				case definition.WorkflowCallEventInputTypeString:
					ty = exprtypes.StringType()
				case definition.WorkflowCallEventInputTypeBoolean:
					ty = exprtypes.BoolType()
					if len(expressions) == 1 && input.Default.IsExpressionAssigned() {
						if k := expressions[0].ty.Kind(); k != exprtypes.KindBool && k != exprtypes.KindAny {
							rule.Errorf(scalarPosition(input.Default), "type of input %q must be bool but found type %s", input.Name.Text(), expressions[0].ty.String())
						}
					}
				case definition.WorkflowCallEventInputTypeNumber:
					ty = exprtypes.NumberType()
					if len(expressions) == 1 && input.Default.IsExpressionAssigned() {
						if k := expressions[0].ty.Kind(); k != exprtypes.KindNumber && k != exprtypes.KindAny {
							rule.Errorf(scalarPosition(input.Default), "type of input %q must be number but found type %s", input.Name.Text(), expressions[0].ty.String())
						}
					}
				case definition.WorkflowCallEventInputTypeInvalid:
				}
				inputs.SetProperty(input.Name.Text(), ty)
				rule.inputsTy = new(inputs.Build())
			}
			// Distinguish absent secrets (inheritance) from a declared empty mapping.
			if e.Secrets != nil {
				var secrets exprtypes.ObjectBuilder
				for id, secret := range e.Secrets {
					name := id
					if secret.Name != nil {
						name = secret.Name.Text()
					}
					secrets.SetProperty(name, exprtypes.StringType())
				}
				if hasNonWorkflowCallTrigger {
					secrets.SetMapped(exprtypes.StringType())
				}
				rule.secretsTy = new(secrets.Build())
			}
			for _, secret := range e.Secrets {
				rule.checkString(secret.Description, "")
			}
			for _, output := range e.Outputs {
				rule.checkString(output.Description, "")
			}
		} else if e, ok := event.(*definition.ImageVersionEvent); ok {
			rule.checkStrings(e.Types, "")
			rule.checkStrings(e.Names, "")
			rule.checkStrings(e.Versions, "")
		}
	}
	rule.checkString(n.RunName, "run-name")
	rule.checkEnv(n.Env, "env")
	rule.checkDefaults(n.Defaults, "")
	rule.checkConcurrency(n.Concurrency, "concurrency")
	return nil
}

func (rule *Checker) VisitWorkflowPost(n workflows.BoundWorkflow) error {
	for _, event := range n.On {
		if call, ok := event.(*definition.WorkflowCallEvent); ok {
			rule.checkWorkflowCallOutputs(call, n)
			break
		}
	}
	rule.workflow = workflows.BoundWorkflow{}
	return nil
}

func (rule *Checker) VisitJobPre(n workflows.BoundJob) error {
	rule.jobReferences = nil
	if rule.options.JobReferences != nil {
		names := rule.options.JobReferences(n)
		rule.jobReferences = &names
	}
	rule.jobEnv, rule.scriptJob = n.Env, n
	rule.matrixScriptSafe = nil
	// Needs must be available before matrix expressions are evaluated.
	rule.needsTy = new(rule.calcNeedsType(n))
	strategy := n.Strategy
	if strategy != nil {
		resolution := rule.matrixResolution(strategy)
		if strategy.Expression != nil {
			rule.checkWorkflowExpression(strategy.Expression, "strategy", "jobs.<job_id>.strategy", schema.Strategy)
			rule.matrixTy = new(resolution.ContextType(nil))
			rule.matrixScriptSafe = safeMatrixScriptReferences(resolution)
		} else if strategy.Matrix != nil {
			rule.matrixTy = new(rule.checkMatrix(strategy.Matrix, resolution))
			rule.matrixScriptSafe = safeMatrixScriptReferences(resolution)
		}
	}
	rule.checkString(n.Name, "jobs.<job_id>.name")
	rule.checkStrings(n.Needs, "")
	runner := n.RunsOn
	if runner != nil {
		rule.checkWorkflowExpression(runner.Expression, "runs-on", "jobs.<job_id>.runs-on", schema.Runner)
		if runner.LabelsExpr != nil {
			if ty, ok := rule.checkOneExpression(runner.LabelsExpr, "runner label at \"runs-on\" section", "jobs.<job_id>.runs-on"); ok {
				switch ty.Kind() {
				case exprtypes.KindArray, exprtypes.KindString, exprtypes.KindAny:
					if literal, known := expressionLiteral(runner.LabelsExpr.Text()); known {
						if labels, ok := literal.([]any); ok && len(labels) == 0 {
							rule.Error(scalarPosition(runner.LabelsExpr), "runs-on.labels must contain at least one value")
						}
					}
				case exprtypes.KindNull, exprtypes.KindNumber, exprtypes.KindBool, exprtypes.KindObject:
					rule.Errorf(scalarPosition(runner.LabelsExpr), "type of expression at \"runs-on\" must be string or array but found type %q", ty.String())
				}
			}
		} else {
			rule.checkStrings(runner.Labels, "jobs.<job_id>.runs-on")
		}
		rule.checkString(runner.Group, "jobs.<job_id>.runs-on")
	}
	rule.checkConcurrency(n.Concurrency, "jobs.<job_id>.concurrency")
	rule.checkEnv(n.Env, "jobs.<job_id>.env")
	rule.checkDefaults(n.Defaults, "jobs.<job_id>.defaults.run")
	rule.checkIfCondition(n.If, "jobs.<job_id>.if")
	if strategy != nil {
		rule.checkBool(strategy.FailFast, "jobs.<job_id>.strategy")
		rule.checkMaxParallel(strategy.MaxParallel, "jobs.<job_id>.strategy")
	}
	rule.checkBool(n.ContinueOnError, "jobs.<job_id>.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes, "jobs.<job_id>.timeout-minutes")
	rule.checkFloat(n.CancelTimeoutMinutes, "jobs.<job_id>.cancel-timeout-minutes")
	rule.checkContainer(n.Container, "jobs.<job_id>.container", "")
	services := n.Services
	if services != nil {
		rule.checkWorkflowExpression(services.Expression, "services", "jobs.<job_id>.services", schema.Services)
		for _, service := range services.Value {
			rule.checkContainer(service.Container, "jobs.<job_id>.services", "<service_id>")
		}
	}
	rule.checkWorkflowCall(n.WorkflowCall)
	rule.stepsTy = new(exprtypes.NewEmptyStrictObjectType())
	return nil
}

func (rule *Checker) VisitJobPost(n workflows.BoundJob) error {
	defer func() {
		rule.jobEnv, rule.scriptJob = nil, workflows.BoundJob{}
		rule.jobReferences = nil
	}()
	rule.checkSnapshot(n.Snapshot)
	environment := n.Environment
	if environment != nil {
		rule.checkWorkflowExpression(environment.Expression, "environment", "jobs.<job_id>.environment", schema.Environment)
		rule.checkString(environment.Name, "jobs.<job_id>.environment")
		rule.checkString(environment.URL, "jobs.<job_id>.environment.url")
		rule.checkBool(environment.Deployment, "jobs.<job_id>.environment")
	}
	for _, output := range n.Outputs {
		rule.checkString(output.Value, "jobs.<job_id>.outputs.<output_id>")
	}
	rule.matrixTy, rule.matrixScriptSafe, rule.stepsTy, rule.needsTy = nil, nil, nil, nil
	return nil
}

func (rule *Checker) VisitStep(n workflows.BoundStep) error {
	rule.scriptEnv = n.Env
	defer func() { rule.scriptEnv = nil }()
	rule.checkString(n.Name, "jobs.<job_id>.steps.name")
	rule.checkIfCondition(n.If, "jobs.<job_id>.steps.if")
	var spec *definition.String
	if run, ok := n.Run(); ok {
		rule.checkScriptString(run.Run, "jobs.<job_id>.steps.run", rule.scriptShell(n))
		rule.checkString(run.Shell, "")
		rule.checkString(run.WorkingDirectory, "jobs.<job_id>.steps.working-directory")
	} else if action, ok := n.Action(); ok {
		rule.checkString(action.Uses, "")
		for _, name := range action.InputNameExpressions {
			rule.checkString(name, "jobs.<job_id>.steps.with")
		}
		rule.checkActionInputs(action, action.Uses)
		if !action.EntrypointEvaluated {
			rule.checkString(action.Entrypoint, "jobs.<job_id>.steps.with")
		}
		if !action.ArgsEvaluated {
			rule.checkString(action.Args, "jobs.<job_id>.steps.with")
		}
		spec = action.Uses
	}
	rule.checkEnv(n.Env, "jobs.<job_id>.steps.env")
	rule.checkBool(n.ContinueOnError, "jobs.<job_id>.steps.continue-on-error")
	rule.checkBool(n.Background, "jobs.<job_id>.steps.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes, "jobs.<job_id>.steps.timeout-minutes")
	if n.ID != nil {
		steps := *rule.stepsTy
		if n.ID.ContainsExpression() {
			rule.checkString(n.ID, "")
			// An evaluated step ID can name any step, so steps.<id> stays open.
			steps = steps.WithMapped(exprtypes.AnyType())
		}
		step := outputs.StepType(rule.getActionOutputsType(spec))
		rule.stepsTy = new(steps.WithProperty(n.ID.Text(), step.Type()))
	}
	return nil
}
