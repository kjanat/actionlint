package expressions

import (
	exprlang "actionlint.kjanat.dev/internal/expression"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/eventpayload"
	"actionlint.kjanat.dev/internal/workflow/schema"
)

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *Checker) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.workflow = n
	rule.eventTy = eventpayload.Type(n)
	rule.checkString(n.Name, "")
	rule.checkString(n.Description, "")
	hasNonWorkflowCallTrigger := len(n.On) > 1

	for _, e := range n.On {
		switch e := e.(type) {
		case *workflowlang.WebhookEvent:
			rule.checkStrings(e.Types, "")
			rule.checkWebhookEventFilter(e.Branches)
			rule.checkWebhookEventFilter(e.BranchesIgnore)
			rule.checkWebhookEventFilter(e.Tags)
			rule.checkWebhookEventFilter(e.TagsIgnore)
			rule.checkWebhookEventFilter(e.Paths)
			rule.checkWebhookEventFilter(e.PathsIgnore)
			rule.checkStrings(e.Workflows, "")
		case *workflowlang.ScheduledEvent:
			for _, s := range e.Schedules {
				rule.checkString(s.Cron, "")
				rule.checkString(s.Timezone, "")
			}
		case *workflowlang.WorkflowDispatchEvent:
			ity := exprlang.NewEmptyStrictObjectType()
			for id, i := range e.Inputs {
				rule.checkString(i.Description, "")
				rule.checkString(i.Default, "")
				rule.checkBool(i.Required, "")
				rule.checkStrings(i.Options, "")

				var ty exprlang.Type
				switch i.Type {
				case workflowlang.WorkflowDispatchEventInputTypeBoolean:
					ty = exprlang.BoolType{}
				case workflowlang.WorkflowDispatchEventInputTypeNumber:
					ty = exprlang.NumberType{}
				case workflowlang.WorkflowDispatchEventInputTypeString, workflowlang.WorkflowDispatchEventInputTypeChoice, workflowlang.WorkflowDispatchEventInputTypeEnvironment:
					ty = exprlang.StringType{}
				default:
					ty = exprlang.AnyType{}
				}
				name := id
				if i.Name != nil {
					name = i.Name.Value
				}
				ity.SetProperty(name, ty)
			}
			rule.dispatchInputsTy = ity
		case *workflowlang.RepositoryDispatchEvent:
			rule.checkStrings(e.Types, "")
		case *workflowlang.WorkflowCallEvent:
			ity := exprlang.NewEmptyStrictObjectType()

			// Set `inputs` context object before checking inputs since input's default values can refer `inputs` context.
			//   inputs:
			//     input1:
			//       type: string
			//     input2:
			//       type: string
			//       default: ${{ inputs.input1 }}
			rule.inputsTy = ity

			for _, i := range e.Inputs {
				rule.checkString(i.Description, "")
				// Check default value before setting type to `ity` because referring myself should cause an error.
				//   inputs:
				//     recursive:
				//       type: string
				//       default: ${{ inputs.recursive }}
				ts := rule.checkString(i.Default, "on.workflow_call.inputs.<inputs_id>.default")

				var ty exprlang.Type
				switch i.Type {
				case workflowlang.WorkflowCallEventInputTypeString:
					ty = exprlang.StringType{}
				case workflowlang.WorkflowCallEventInputTypeBoolean:
					ty = exprlang.BoolType{}
					if len(ts) == 1 && i.Default.IsExpressionAssigned() {
						switch ts[0].ty.(type) {
						case exprlang.BoolType, exprlang.AnyType:
							// ok
						default:
							rule.Errorf(i.Default.Pos, "type of input %q must be bool but found type %s", i.Name.Value, ts[0].ty.String())
						}
					}
				case workflowlang.WorkflowCallEventInputTypeNumber:
					ty = exprlang.NumberType{}
					if len(ts) == 1 && i.Default.IsExpressionAssigned() {
						switch ts[0].ty.(type) {
						case exprlang.NumberType, exprlang.AnyType:
							// ok
						default:
							rule.Errorf(i.Default.Pos, "type of input %q must be number but found type %s", i.Name.Value, ts[0].ty.String())
						}
					}
				default:
					ty = exprlang.AnyType{}
				}
				ity.SetProperty(i.Name.Value, ty)
			}

			// When no secret is passed, secrets may be inherited from a caller of the workflow.
			// So `secrets` context must be typed as { string => string }. `e.Secrets` is nil when `secrets:` does not
			// exist. When `e.Secrets` is an empty map, `secrets:` exists but it has no child. A workflow with another
			// trigger can also access repository secrets on that trigger path, so its secrets type must remain open.
			if e.Secrets != nil {
				sty := exprlang.NewEmptyStrictObjectType()
				for id, secret := range e.Secrets {
					name := id
					if secret.Name != nil {
						name = secret.Name.Value
					}
					sty.SetProperty(name, exprlang.StringType{})
				}
				if hasNonWorkflowCallTrigger {
					sty.Mapped = exprlang.StringType{}
				}
				rule.secretsTy = sty
			}
			for _, s := range e.Secrets {
				rule.checkString(s.Description, "")
			}

			for _, o := range e.Outputs {
				rule.checkString(o.Description, "")
				// o.Value will be checked in VisitWorkflowPost
			}
		case *workflowlang.ImageVersionEvent:
			rule.checkStrings(e.Types, "")
			rule.checkStrings(e.Names, "")
			rule.checkStrings(e.Versions, "")
		}
	}

	rule.checkString(n.RunName, "run-name")
	rule.checkEnv(n.Env, "env")

	rule.checkDefaults(n.Defaults, "")
	rule.checkConcurrency(n.Concurrency, "concurrency")

	rule.workflow = n
	return nil
}

// VisitWorkflowPost is callback when visiting Workflow node after visiting its children
func (rule *Checker) VisitWorkflowPost(n *workflowlang.Workflow) error {
	if e, ok := n.FindWorkflowCallEvent(); ok {
		rule.checkWorkflowCallOutputs(e.Outputs, n.Jobs)
	}
	rule.workflow = nil
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *Checker) VisitJobPre(n *workflowlang.Job) error {
	rule.matrixScriptSafe = nil
	// Type of needs must be resolved before resolving type of matrix because `needs` context can
	// be used in matrix configuration.
	rule.needsTy = rule.calcNeedsType(n)

	// Set matrix type at start of VisitJobPre() because matrix values are available in
	// jobs.<job_id> section. For example:
	//   jobs:
	//     foo:
	//       strategy:
	//         matrix:
	//           os: [ubuntu-latest, macos-latest, windows-latest]
	//       runs-on: ${{ matrix.os }}
	if n.Strategy != nil {
		if n.Strategy.Expression != nil {
			ty := rule.checkWorkflowExpression(n.Strategy.Expression, "strategy", "jobs.<job_id>.strategy", schema.Strategy)
			rule.matrixTy = exprlang.NewEmptyObjectType()
			if object, ok := ty.(*exprlang.ObjectType); ok {
				rule.matrixTy = schema.MatrixType(schema.Property(object.Props, "matrix"))
			}
		} else if n.Strategy.Matrix != nil {
			// Check and guess type of the matrix.
			rule.matrixTy = rule.checkMatrix(n.Strategy.Matrix)
			rule.matrixScriptSafe = safeMatrixScriptReferences(n.Strategy.Matrix)
		}
	}

	rule.checkString(n.Name, "jobs.<job_id>.name")
	rule.checkStrings(n.Needs, "")

	if n.RunsOn != nil {
		rule.checkWorkflowExpression(n.RunsOn.Expression, "runs-on", "jobs.<job_id>.runs-on", schema.Runner)
		if n.RunsOn.LabelsExpr != nil {
			if ty := rule.checkOneExpression(n.RunsOn.LabelsExpr, "runner label at \"runs-on\" section", "jobs.<job_id>.runs-on"); ty != nil {
				switch ty.(type) {
				case *exprlang.ArrayType, exprlang.StringType, exprlang.AnyType:
					if value, known := workflowlang.WorkflowExpressionLiteral(n.RunsOn.LabelsExpr); known {
						if labels, ok := value.([]any); ok && len(labels) == 0 {
							rule.Error(n.RunsOn.LabelsExpr.Pos, "runs-on.labels must contain at least one value")
						}
					}
				default:
					rule.Errorf(n.RunsOn.LabelsExpr.Pos, "type of expression at \"runs-on\" must be string or array but found type %q", ty.String())
				}
			}
		} else {
			for _, l := range n.RunsOn.Labels {
				rule.checkString(l, "jobs.<job_id>.runs-on")
			}
		}
		rule.checkString(n.RunsOn.Group, "jobs.<job_id>.runs-on")
	}

	rule.checkConcurrency(n.Concurrency, "jobs.<job_id>.concurrency")

	rule.checkEnv(n.Env, "jobs.<job_id>.env")

	rule.checkDefaults(n.Defaults, "jobs.<job_id>.defaults.run")
	rule.checkIfCondition(n.If, "jobs.<job_id>.if")

	if n.Strategy != nil {
		// Note: Types in "jobs.<job_id>.strategy.matrix" were checked `checkMatrix`
		rule.checkBool(n.Strategy.FailFast, "jobs.<job_id>.strategy")
		rule.checkMaxParallel(n.Strategy.MaxParallel, "jobs.<job_id>.strategy")
	}

	rule.checkBool(n.ContinueOnError, "jobs.<job_id>.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes, "jobs.<job_id>.timeout-minutes")
	rule.checkFloat(n.CancelTimeoutMinutes, "jobs.<job_id>.cancel-timeout-minutes")
	rule.checkContainer(n.Container, "jobs.<job_id>.container", "")

	if n.Services != nil {
		rule.checkWorkflowExpression(n.Services.Expression, "services", "jobs.<job_id>.services", schema.Services)
		for _, s := range n.Services.Value {
			rule.checkContainer(s.Container, "jobs.<job_id>.services", "<service_id>")
		}
	}

	rule.checkWorkflowCall(n.WorkflowCall)

	rule.stepsTy = exprlang.NewEmptyStrictObjectType()

	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children
func (rule *Checker) VisitJobPost(n *workflowlang.Job) error {
	// Snapshot conditions run after the job's steps, so their outputs are available.
	rule.checkSnapshot(n.Snapshot)
	// 'environment' and 'outputs' sections are evaluated after all steps are run
	if n.Environment != nil {
		rule.checkWorkflowExpression(n.Environment.Expression, "environment", "jobs.<job_id>.environment", schema.Environment)
		rule.checkString(n.Environment.Name, "jobs.<job_id>.environment")
		rule.checkString(n.Environment.URL, "jobs.<job_id>.environment.url")
		rule.checkBool(n.Environment.Deployment, "jobs.<job_id>.environment")
	}
	for _, output := range n.Outputs {
		rule.checkString(output.Value, "jobs.<job_id>.outputs.<output_id>")
	}

	rule.matrixTy = nil
	rule.matrixScriptSafe = nil
	rule.stepsTy = nil
	rule.needsTy = nil

	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *Checker) VisitStep(n *workflowlang.Step) error {
	rule.scriptEnv = n.Env
	defer func() { rule.scriptEnv = nil }()
	rule.checkString(n.Name, "jobs.<job_id>.steps.name")
	rule.checkIfCondition(n.If, "jobs.<job_id>.steps.if")

	var spec *workflowlang.String
	switch e := n.Exec.(type) {
	case *workflowlang.ExecRun:
		rule.checkScriptString(e.Run, "jobs.<job_id>.steps.run")
		rule.checkString(e.Shell, "")
		rule.checkString(e.WorkingDirectory, "jobs.<job_id>.steps.working-directory")
	case *workflowlang.ExecAction:
		rule.checkString(e.Uses, "")
		for _, name := range e.InputNameExpressions {
			rule.checkString(name, "jobs.<job_id>.steps.with")
		}
		rule.checkActionInputs(workflowlang.InputMapping(e), e.Uses)
		if !e.EntrypointEvaluated {
			rule.checkString(e.Entrypoint, "jobs.<job_id>.steps.with")
		}
		if !e.ArgsEvaluated {
			rule.checkString(e.Args, "jobs.<job_id>.steps.with")
		}
		spec = e.Uses
	}

	rule.checkEnv(n.Env, "jobs.<job_id>.steps.env") // env: at step level can refer 'env' context (#158)
	rule.checkBool(n.ContinueOnError, "jobs.<job_id>.steps.continue-on-error")
	// Preserve the background-expression extension with the continue-on-error profile.
	rule.checkBool(n.Background, "jobs.<job_id>.steps.continue-on-error")
	rule.checkFloat(n.TimeoutMinutes, "jobs.<job_id>.steps.timeout-minutes")

	if n.ID != nil {
		if n.ID.ContainsExpression() {
			rule.checkString(n.ID, "")
			rule.stepsTy.Loose()
		}
		// Step ID is case insensitive
		rule.stepsTy.SetProperty(n.ID.Value, exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs":    rule.getActionOutputsType(spec),
			"conclusion": exprlang.StringType{},
			"outcome":    exprlang.StringType{},
		}))
	}

	return nil
}
