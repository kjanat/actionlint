package expressions

import (
	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/eventpayload"
	"actionlint.kjanat.dev/v2/internal/workflow/schema"
)

func (rule *Checker) VisitWorkflowPre(n workflowdecl.WorkflowView) error {
	rule.workflow = n
	rule.eventTy = new(eventpayload.Type(n))
	rule.checkString(n.Name(), "")
	rule.checkString(n.Description(), "")
	triggerCount := 0
	for range n.On() {
		triggerCount++
	}
	hasNonWorkflowCallTrigger := triggerCount > 1
	for event := range n.On() {
		if e, ok := event.WebhookEvent(); ok {
			rule.checkStrings(e.Types(), "")
			for _, filter := range []workflowdecl.WebhookEventFilterView{e.Branches(), e.BranchesIgnore(), e.Tags(), e.TagsIgnore(), e.Paths(), e.PathsIgnore()} {
				rule.checkWebhookEventFilter(filter)
			}
			rule.checkStrings(e.Workflows(), "")
		} else if e, ok := event.ScheduledEvent(); ok {
			for schedule := range e.Schedules() {
				rule.checkString(schedule.Cron(), "")
				rule.checkString(schedule.Timezone(), "")
			}
		} else if e, ok := event.WorkflowDispatchEvent(); ok {
			var inputs exprlang.ObjectBuilder
			for id, input := range e.Inputs() {
				rule.checkString(input.Description(), "")
				rule.checkString(input.Default(), "")
				rule.checkBool(input.Required(), "")
				rule.checkStrings(input.Options(), "")
				ty := exprlang.AnyType()
				switch input.Type() {
				case workflowlang.WorkflowDispatchEventInputTypeBoolean:
					ty = exprlang.BoolType()
				case workflowlang.WorkflowDispatchEventInputTypeNumber:
					ty = exprlang.NumberType()
				case workflowlang.WorkflowDispatchEventInputTypeString, workflowlang.WorkflowDispatchEventInputTypeChoice, workflowlang.WorkflowDispatchEventInputTypeEnvironment:
					ty = exprlang.StringType()
				case workflowlang.WorkflowDispatchEventInputTypeNone:
				}
				name := id
				if input.Name().Present() {
					name = input.Name().Value()
				}
				inputs.SetProperty(name, ty)
			}
			rule.dispatchInputsTy = new(inputs.Build())
		} else if e, ok := event.RepositoryDispatchEvent(); ok {
			rule.checkStrings(e.Types(), "")
		} else if e, ok := event.WorkflowCallEvent(); ok {
			var inputs exprlang.ObjectBuilder
			// Defaults can use earlier inputs, but cannot refer to their own
			// definition, so rule.inputsTy is rebuilt after each input is added.
			rule.inputsTy = new(inputs.Build())
			for input := range e.Inputs() {
				rule.checkString(input.Description(), "")
				expressions := rule.checkString(input.Default(), "on.workflow_call.inputs.<inputs_id>.default")
				ty := exprlang.AnyType()
				switch input.Type() {
				case workflowlang.WorkflowCallEventInputTypeString:
					ty = exprlang.StringType()
				case workflowlang.WorkflowCallEventInputTypeBoolean:
					ty = exprlang.BoolType()
					if len(expressions) == 1 && input.Default().IsExpressionAssigned() {
						if k := expressions[0].ty.Kind(); k != exprlang.KindBool && k != exprlang.KindAny {
							rule.Errorf(viewPosition(input.Default()), "type of input %q must be bool but found type %s", input.Name().Value(), expressions[0].ty.String())
						}
					}
				case workflowlang.WorkflowCallEventInputTypeNumber:
					ty = exprlang.NumberType()
					if len(expressions) == 1 && input.Default().IsExpressionAssigned() {
						if k := expressions[0].ty.Kind(); k != exprlang.KindNumber && k != exprlang.KindAny {
							rule.Errorf(viewPosition(input.Default()), "type of input %q must be number but found type %s", input.Name().Value(), expressions[0].ty.String())
						}
					}
				case workflowlang.WorkflowCallEventInputTypeInvalid:
				}
				inputs.SetProperty(input.Name().Value(), ty)
				rule.inputsTy = new(inputs.Build())
			}
			// Distinguish absent secrets (inheritance) from a declared empty mapping.
			if e.SecretsPresent() {
				var secrets exprlang.ObjectBuilder
				for id, secret := range e.Secrets() {
					name := id
					if secret.Name().Present() {
						name = secret.Name().Value()
					}
					secrets.SetProperty(name, exprlang.StringType())
				}
				if hasNonWorkflowCallTrigger {
					secrets.SetMapped(exprlang.StringType())
				}
				rule.secretsTy = new(secrets.Build())
			}
			for _, secret := range e.Secrets() {
				rule.checkString(secret.Description(), "")
			}
			for _, output := range e.Outputs() {
				rule.checkString(output.Description(), "")
			}
		} else if e, ok := event.ImageVersionEvent(); ok {
			rule.checkStrings(e.Types(), "")
			rule.checkStrings(e.Names(), "")
			rule.checkStrings(e.Versions(), "")
		}
	}
	rule.checkString(n.RunName(), "run-name")
	rule.checkEnv(n.Env(), "env")
	rule.checkDefaults(n.Defaults(), "")
	rule.checkConcurrency(n.Concurrency(), "concurrency")
	return nil
}

func (rule *Checker) VisitWorkflowPost(n workflowdecl.WorkflowView) error {
	for event := range n.On() {
		if call, ok := event.WorkflowCallEvent(); ok {
			rule.checkWorkflowCallOutputs(call, n)
			break
		}
	}
	rule.workflow = workflowdecl.WorkflowView{}
	return nil
}

func (rule *Checker) VisitJobPre(n workflowdecl.JobView) error {
	rule.jobReferences = nil
	if rule.options.JobReferences != nil {
		names := rule.options.JobReferences(n)
		rule.jobReferences = &names
	}
	rule.jobEnv, rule.scriptJob = n.Env(), n
	rule.matrixScriptSafe = nil
	// Needs must be available before matrix expressions are evaluated.
	rule.needsTy = new(rule.calcNeedsType(n))
	strategy := n.Strategy()
	if strategy.Present() {
		resolution := rule.matrixResolution(strategy)
		if strategy.Expression().Present() {
			rule.checkWorkflowExpression(strategy.Expression(), "strategy", "jobs.<job_id>.strategy", schema.Strategy)
			rule.matrixTy = new(matrixTypeFromResolution(resolution))
			rule.matrixScriptSafe = safeMatrixScriptReferences(resolution)
		} else if strategy.Matrix().Present() {
			rule.matrixTy = new(rule.checkMatrix(strategy.Matrix(), resolution))
			rule.matrixScriptSafe = safeMatrixScriptReferences(resolution)
		}
	}
	rule.checkString(n.Name(), "jobs.<job_id>.name")
	rule.checkStrings(n.Needs(), "")
	runner := n.RunsOn()
	if runner.Present() {
		rule.checkWorkflowExpression(runner.Expression(), "runs-on", "jobs.<job_id>.runs-on", schema.Runner)
		if runner.LabelsExpr().Present() {
			if ty, ok := rule.checkOneExpression(runner.LabelsExpr(), "runner label at \"runs-on\" section", "jobs.<job_id>.runs-on"); ok {
				switch ty.Kind() {
				case exprlang.KindArray, exprlang.KindString, exprlang.KindAny:
					if literal, known := expressionLiteral(runner.LabelsExpr().Value()); known {
						if labels, ok := literal.([]any); ok && len(labels) == 0 {
							rule.Error(viewPosition(runner.LabelsExpr()), "runs-on.labels must contain at least one value")
						}
					}
				case exprlang.KindNull, exprlang.KindNumber, exprlang.KindBool, exprlang.KindObject:
					rule.Errorf(viewPosition(runner.LabelsExpr()), "type of expression at \"runs-on\" must be string or array but found type %q", ty.String())
				}
			}
		} else {
			rule.checkStrings(runner.Labels(), "jobs.<job_id>.runs-on")
		}
		rule.checkString(runner.Group(), "jobs.<job_id>.runs-on")
	}
	rule.checkConcurrency(n.Concurrency(), "jobs.<job_id>.concurrency")
	rule.checkEnv(n.Env(), "jobs.<job_id>.env")
	rule.checkDefaults(n.Defaults(), "jobs.<job_id>.defaults.run")
	rule.checkIfCondition(n.If(), "jobs.<job_id>.if")
	if strategy.Present() {
		rule.checkBool(strategy.FailFast(), "jobs.<job_id>.strategy")
		rule.checkMaxParallel(strategy.MaxParallel(), "jobs.<job_id>.strategy")
	}
	rule.checkBool(n.ContinueOnError(), "jobs.<job_id>.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes(), "jobs.<job_id>.timeout-minutes")
	rule.checkFloat(n.CancelTimeoutMinutes(), "jobs.<job_id>.cancel-timeout-minutes")
	rule.checkContainer(n.Container(), "jobs.<job_id>.container", "")
	services := n.Services()
	if services.Present() {
		rule.checkWorkflowExpression(services.Expression(), "services", "jobs.<job_id>.services", schema.Services)
		for _, service := range services.Value() {
			rule.checkContainer(service.Container(), "jobs.<job_id>.services", "<service_id>")
		}
	}
	rule.checkWorkflowCall(n.WorkflowCall())
	rule.stepsTy = new(exprlang.NewEmptyStrictObjectType())
	return nil
}

func (rule *Checker) VisitJobPost(n workflowdecl.JobView) error {
	defer func() {
		rule.jobEnv, rule.scriptJob = workflowdecl.EnvView{}, workflowdecl.JobView{}
		rule.jobReferences = nil
	}()
	rule.checkSnapshot(n.Snapshot())
	environment := n.Environment()
	if environment.Present() {
		rule.checkWorkflowExpression(environment.Expression(), "environment", "jobs.<job_id>.environment", schema.Environment)
		rule.checkString(environment.Name(), "jobs.<job_id>.environment")
		rule.checkString(environment.URL(), "jobs.<job_id>.environment.url")
		rule.checkBool(environment.Deployment(), "jobs.<job_id>.environment")
	}
	for _, output := range n.Outputs() {
		rule.checkString(output.Value(), "jobs.<job_id>.outputs.<output_id>")
	}
	rule.matrixTy, rule.matrixScriptSafe, rule.stepsTy, rule.needsTy = nil, nil, nil, nil
	return nil
}

func (rule *Checker) VisitStep(n workflowdecl.StepView) error {
	rule.scriptEnv = n.Env()
	defer func() { rule.scriptEnv = workflowdecl.EnvView{} }()
	rule.checkString(n.Name(), "jobs.<job_id>.steps.name")
	rule.checkIfCondition(n.If(), "jobs.<job_id>.steps.if")
	var spec workflowdecl.StringView
	if run, ok := n.Run(); ok {
		rule.checkScriptString(run.Run(), "jobs.<job_id>.steps.run", rule.scriptShell(n))
		rule.checkString(run.Shell(), "")
		rule.checkString(run.WorkingDirectory(), "jobs.<job_id>.steps.working-directory")
	} else if action, ok := n.Action(); ok {
		rule.checkString(action.Uses(), "")
		for name := range action.InputNameExpressions() {
			rule.checkString(name, "jobs.<job_id>.steps.with")
		}
		rule.checkActionInputs(action, action.Uses())
		if !action.EntrypointEvaluated() {
			rule.checkString(action.Entrypoint(), "jobs.<job_id>.steps.with")
		}
		if !action.ArgsEvaluated() {
			rule.checkString(action.Args(), "jobs.<job_id>.steps.with")
		}
		spec = action.Uses()
	}
	rule.checkEnv(n.Env(), "jobs.<job_id>.steps.env")
	rule.checkBool(n.ContinueOnError(), "jobs.<job_id>.steps.continue-on-error")
	rule.checkBool(n.Background(), "jobs.<job_id>.steps.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes(), "jobs.<job_id>.steps.timeout-minutes")
	if n.ID().Present() {
		steps := *rule.stepsTy
		if n.ID().ContainsExpression() {
			rule.checkString(n.ID(), "")
			// An evaluated step ID can name any step, so steps.<id> stays open.
			steps = steps.WithMapped(exprlang.AnyType())
		}
		step := exprlang.NewStrictObjectType(map[string]exprlang.Type{"outputs": rule.getActionOutputsType(spec).Type(), "conclusion": exprlang.StringType(), "outcome": exprlang.StringType()})
		rule.stepsTy = new(steps.WithProperty(n.ID().Value(), step.Type()))
	}
	return nil
}
