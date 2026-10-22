package engine

import (
	"strconv"
	"strings"

	exprlang "actionlint.kjanat.dev/internal/expression"
	"actionlint.kjanat.dev/internal/mapping"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	workflowcontext "actionlint.kjanat.dev/internal/workflowcontext"
	yamlscalar "actionlint.kjanat.dev/internal/yamlscalar"
)

type typedExpr struct {
	ty  exprlang.Type
	pos workflowlang.Pos
}

// RuleExpression is a rule checker to check expression syntax in string values of workflow syntax.
// It checks syntax and semantics of the expressions including type checks and functions/contexts
// definitions. For more details see
// - https://docs.github.com/en/actions/learn-github-actions/contexts
// - https://docs.github.com/en/actions/learn-github-actions/expressions
type RuleExpression struct {
	RuleBase
	matrixTy         *exprlang.ObjectType
	stepsTy          *exprlang.ObjectType
	needsTy          *exprlang.ObjectType
	secretsTy        *exprlang.ObjectType
	inputsTy         *exprlang.ObjectType
	dispatchInputsTy *exprlang.ObjectType
	jobsTy           *exprlang.ObjectType
	workflow         *workflowlang.Workflow
	localActions     *LocalActionsCache
	localWorkflows   *LocalReusableWorkflowCache
	expressionSource *workflowlang.ScriptSource
	expressionOffset int
}

// NewRuleExpression creates new RuleExpression instance.
func NewRuleExpression(actionsCache *LocalActionsCache, workflowCache *LocalReusableWorkflowCache) *RuleExpression {
	return &RuleExpression{
		RuleBase:         builtinRuleBase("expression"),
		matrixTy:         nil,
		stepsTy:          nil,
		needsTy:          nil,
		secretsTy:        nil,
		inputsTy:         nil,
		dispatchInputsTy: nil,
		jobsTy:           nil,
		workflow:         nil,
		localActions:     actionsCache,
		localWorkflows:   workflowCache,
	}
}

// VisitWorkflowPre is callback when visiting Workflow node before visiting its children.
func (rule *RuleExpression) VisitWorkflowPre(n *workflowlang.Workflow) error {
	rule.workflow = n
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
				ity.Props[id] = ty
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
				ity.Props[i.ID] = ty
			}

			// When no secret is passed, secrets may be inherited from a caller of the workflow.
			// So `secrets` context must be typed as { string => string }. `e.Secrets` is nil when `secrets:` does not
			// exist. When `e.Secrets` is an empty map, `secrets:` exists but it has no child. A workflow with another
			// trigger can also access repository secrets on that trigger path, so its secrets type must remain open.
			if e.Secrets != nil {
				sty := exprlang.NewEmptyStrictObjectType()
				for id := range e.Secrets {
					sty.Props[id] = exprlang.StringType{}
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
func (rule *RuleExpression) VisitWorkflowPost(n *workflowlang.Workflow) error {
	if e, ok := n.FindWorkflowCallEvent(); ok {
		rule.checkWorkflowCallOutputs(e.Outputs, n.Jobs)
	}
	rule.workflow = nil
	return nil
}

// VisitJobPre is callback when visiting Job node before visiting its children.
func (rule *RuleExpression) VisitJobPre(n *workflowlang.Job) error {
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
			ty := rule.checkWorkflowExpression(n.Strategy.Expression, "strategy", "jobs.<job_id>.strategy", workflowStrategy)
			rule.matrixTy = exprlang.NewEmptyObjectType()
			if object, ok := ty.(*exprlang.ObjectType); ok {
				rule.matrixTy = matrixTypeFromExpression(workflowObjectProperty(object.Props, "matrix"))
			}
		} else if n.Strategy.Matrix != nil {
			// Check and guess type of the matrix.
			rule.matrixTy = rule.checkMatrix(n.Strategy.Matrix)
		}
	}

	rule.checkString(n.Name, "jobs.<job_id>.name")
	rule.checkStrings(n.Needs, "")

	if n.RunsOn != nil {
		rule.checkWorkflowExpression(n.RunsOn.Expression, "runs-on", "jobs.<job_id>.runs-on", workflowRunner)
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
		rule.checkWorkflowExpression(n.Services.Expression, "services", "jobs.<job_id>.services", workflowExpressionObject{mapped: workflowContainerShape(true)})
		for _, s := range n.Services.Value {
			rule.checkContainer(s.Container, "jobs.<job_id>.services", "<service_id>")
		}
	}

	rule.checkWorkflowCall(n.WorkflowCall)

	rule.stepsTy = exprlang.NewEmptyStrictObjectType()

	return nil
}

// VisitJobPost is callback when visiting Job node after visiting its children
func (rule *RuleExpression) VisitJobPost(n *workflowlang.Job) error {
	// Snapshot conditions run after the job's steps, so their outputs are available.
	rule.checkSnapshot(n.Snapshot)
	// 'environment' and 'outputs' sections are evaluated after all steps are run
	if n.Environment != nil {
		rule.checkWorkflowExpression(n.Environment.Expression, "environment", "jobs.<job_id>.environment", workflowEnvironment)
		rule.checkString(n.Environment.Name, "jobs.<job_id>.environment")
		rule.checkString(n.Environment.URL, "jobs.<job_id>.environment.url")
		rule.checkBool(n.Environment.Deployment, "jobs.<job_id>.environment")
	}
	for _, output := range n.Outputs {
		rule.checkString(output.Value, "jobs.<job_id>.outputs.<output_id>")
	}

	rule.matrixTy = nil
	rule.stepsTy = nil
	rule.needsTy = nil

	return nil
}

// VisitStep is callback when visiting Step node.
func (rule *RuleExpression) VisitStep(n *workflowlang.Step) error {
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
		id := strings.ToLower(n.ID.Value)
		rule.stepsTy.Props[id] = exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs":    rule.getActionOutputsType(spec),
			"conclusion": exprlang.StringType{},
			"outcome":    exprlang.StringType{},
		})
	}

	return nil
}

// Get type of `outputs.<output name>`
func (rule *RuleExpression) getActionOutputsType(spec *workflowlang.String) *exprlang.ObjectType {
	if spec == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	localSpec := spec.Value
	if s, ok := selfRepositoryUsesLocalSpec(localSpec); ok {
		localSpec = s
	}
	if strings.HasPrefix(localSpec, "./") {
		meta, _, err := rule.localActions.FindMetadata(spec.Value)
		if err != nil {
			// The action rule owns metadata failures; outputs remain unknown.
			rule.Debug("Could not infer action outputs: %v", err)
			return exprlang.NewMapObjectType(exprlang.StringType{})
		}
		if meta == nil {
			return exprlang.NewMapObjectType(exprlang.StringType{})
		}

		return typeOfActionOutputs(meta)
	}

	// github-script action allows to set any outputs through calling `core.setOutput` directly.
	// So any `outputs.*` properties should be accepted (#104)
	if strings.HasPrefix(spec.Value, "actions/github-script@") {
		return exprlang.NewEmptyObjectType()
	}

	// When the action run at this step is a popular action, we know what outputs are set by it.
	// Set the output names to `steps.{step_id}.outputs.{name}`.
	if meta, _ := lookupPopularAction(spec.Value); meta != nil {
		return typeOfActionOutputs(meta)
	}

	return exprlang.NewMapObjectType(exprlang.StringType{})
}

func (rule *RuleExpression) getWorkflowCallOutputsType(call *workflowlang.WorkflowCall) *exprlang.ObjectType {
	if call.Uses == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	m, err := rule.localWorkflows.findMetadataForCall(call.Uses.Value)
	if err != nil {
		// The workflow-call rule owns metadata failures; outputs remain unknown.
		rule.Debug("Could not infer workflow outputs: %v", err)
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}
	if m == nil {
		return exprlang.NewMapObjectType(exprlang.StringType{})
	}

	p := make(map[string]exprlang.Type, len(m.Outputs))
	for n := range m.Outputs {
		p[n] = exprlang.StringType{}
	}
	return exprlang.NewStrictObjectType(p)
}

func (rule *RuleExpression) checkOneExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	// checkString is not available since it checks types for embedding values into a string
	if s == nil {
		return nil
	}

	ts, ok := rule.checkExprsIn(s.Value, s.Pos, s.Quoted, false, workflowKey)
	if !ok {
		return nil
	}

	if len(ts) != 1 {
		// This case should be unreachable since only one ${{ }} is included is checked by parser
		rule.Errorf(s.Pos, "one ${{ }} expression should be included in %q value but got %d expressions", what, len(ts))
		return nil
	}

	if value, known := workflowlang.WorkflowExpressionLiteral(s); known {
		return exprlang.TypeOfJSONValue(value)
	}
	return ts[0].ty
}

func (rule *RuleExpression) checkObjectTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case *exprlang.ObjectType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be object but found type %s", what, ty.String())
		return nil
	}
}

func (rule *RuleExpression) checkArrayTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case *exprlang.ArrayType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be array but found type %s", what, ty.String())
		return nil
	}
}

func (rule *RuleExpression) checkNumberTy(ty exprlang.Type, pos *workflowlang.Pos, what string) exprlang.Type {
	if ty == nil {
		return nil
	}
	switch ty.(type) {
	case exprlang.NumberType, exprlang.AnyType:
		return ty
	default:
		rule.Errorf(pos, "type of expression at %q must be number but found type %s", what, ty.String())
		return nil
	}
}

func (rule *RuleExpression) checkObjectExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkObjectTy(ty, s.Pos, what)
}

func (rule *RuleExpression) checkArrayExpression(s *workflowlang.String, what, workflowKey string) exprlang.Type {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return nil
	}
	return rule.checkArrayTy(ty, s.Pos, what)
}

func (rule *RuleExpression) checkNumberExpression(s *workflowlang.String, what, workflowKey string) {
	ty := rule.checkOneExpression(s, what, workflowKey)
	if ty == nil {
		return
	}
	rule.checkNumberTy(ty, s.Pos, what)
}

func (rule *RuleExpression) checkActionInputs(inputs *mapping.Inputs[*workflowlang.String], uses *workflowlang.String) {
	const key = "jobs.<job_id>.steps.with"
	var action string
	if uses != nil {
		action = uses.Value
	}
	workflowlang.InputResolver.Check(inputs, action, mapping.Checks[*workflowlang.String]{
		Mapping: func(source *workflowlang.String) {
			rule.checkWorkflowExpression(source, "with", key, workflowStringMap)
		},
		String: func(source *workflowlang.String) { rule.checkString(source, key) },
		Script: func(source *workflowlang.String) { rule.checkScriptString(source, key) },
	})
}

func (rule *RuleExpression) checkEnv(env *workflowlang.Env, workflowKey string) {
	if env == nil {
		return
	}

	workflowlang.InputResolver.Check(workflowlang.EnvMappingView(env), "", mapping.Checks[*workflowlang.String]{
		Mapping: func(source *workflowlang.String) {
			rule.checkWorkflowExpression(source, "env", workflowKey, workflowStringMap)
		},
		String: func(source *workflowlang.String) { rule.checkString(source, workflowKey) },
	})
}

func (rule *RuleExpression) checkContainer(c *workflowlang.Container, workflowKey, childWorkflowKeyPrefix string) {
	if c == nil {
		return
	}
	rule.checkWorkflowExpression(c.Expression, "container", workflowKey, workflowContainerShape(childWorkflowKeyPrefix != ""))
	childWorkflowKey := workflowKey
	if childWorkflowKeyPrefix != "" {
		childWorkflowKey += "." + childWorkflowKeyPrefix
	}
	rule.checkString(c.Image, workflowKey)
	if c.Credentials != nil {
		k := childWorkflowKey + ".credentials" // e.g. jobs.<job_id>.container.credentials
		if c.Credentials.Expression != nil {
			rule.checkWorkflowExpression(c.Credentials.Expression, "credentials", k, workflowCredentials)
		} else {
			rule.checkString(c.Credentials.Username, k)
			rule.checkString(c.Credentials.Password, k)
		}
	}
	rule.checkEnv(c.Env, childWorkflowKey+".env.<env_id>") // e.g. jobs.<job_id>.container.env.<env_id>
	rule.checkStrings(c.Ports, workflowKey)
	rule.checkStrings(c.Volumes, workflowKey)
	rule.checkWorkflowExpression(c.PortsExpression, "ports", workflowKey, workflowExpressionArray{elem: workflowNonEmpty})
	rule.checkWorkflowExpression(c.VolumesExpression, "volumes", workflowKey, workflowExpressionArray{elem: workflowNonEmpty})
	rule.checkString(c.Options, workflowKey)
	rule.checkString(c.Command, workflowKey)
	rule.checkString(c.Entrypoint, workflowKey)
}

func (rule *RuleExpression) checkConcurrency(c *workflowlang.Concurrency, workflowKey string) {
	if c == nil {
		return
	}
	shape := workflowConcurrency
	if workflowKey == "jobs.<job_id>.concurrency" {
		shape = workflowJobConcurrency
	}
	rule.checkWorkflowExpression(c.Expression, "concurrency", workflowKey, shape)
	if c.Group != nil && c.Group.IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Group, "concurrency.group", workflowKey, workflowNonEmpty)
	} else {
		rule.checkString(c.Group, workflowKey)
	}
	if c.Queue != nil && c.Queue.IsExpressionAssigned() {
		rule.checkWorkflowExpression(c.Queue, "concurrency.queue", workflowKey, workflowQueue)
	} else {
		rule.checkString(c.Queue, workflowKey)
	}
	rule.checkBool(c.CancelInProgress, workflowKey)
	if c.Queue == nil || c.CancelInProgress == nil {
		return
	}
	// Literal YAML conflicts are diagnosed by the parser. Resolve known
	// expression values here, retaining unknown values for runtime validation.
	if !c.Queue.ContainsExpression() && c.CancelInProgress.Expression == nil {
		return
	}
	var queue any = c.Queue.Value
	if c.Queue.ContainsExpression() {
		queue, _ = workflowlang.WorkflowExpressionLiteral(c.Queue)
	}
	var cancel any = c.CancelInProgress.Value
	if c.CancelInProgress.Expression != nil {
		cancel, _ = workflowlang.WorkflowExpressionLiteral(c.CancelInProgress.Expression)
	}
	if queue == "max" && cancel == true {
		rule.Error(c.Queue.Pos, `"queue: max" cannot be combined with "cancel-in-progress: true" in "concurrency" section`)
	}
}

func (rule *RuleExpression) checkDefaults(d *workflowlang.Defaults, workflowKey string) {
	if d == nil || d.Run == nil {
		return
	}
	rule.checkWorkflowExpression(d.Run.Expression, "defaults.run", workflowKey, workflowDefaultsRun)
	rule.checkString(d.Run.Shell, workflowKey)
	rule.checkString(d.Run.WorkingDirectory, workflowKey)
}

func (rule *RuleExpression) checkWorkflowCall(c *workflowlang.WorkflowCall) {
	if c == nil || c.Uses == nil {
		return
	}

	rule.checkString(c.Uses, "")

	m, err := rule.localWorkflows.findMetadataForCall(c.Uses.Value)
	if err != nil {
		// The workflow-call rule reports metadata failures at the uses value.
		rule.Debug("Could not infer workflow input types: %v", err)
	}

	for n, i := range c.Inputs {
		ts := rule.checkString(i.Value, "jobs.<job_id>.with.<with_id>")

		if m == nil {
			continue
		}

		mi, ok := m.Inputs[n]
		if !ok || mi == nil {
			continue
		}
		if _, ok := mi.Type.(exprlang.AnyType); ok {
			continue
		}

		v := strings.TrimSpace(i.Value.Value)

		var ty exprlang.Type = exprlang.StringType{}
		switch len(ts) {
		case 0:
			switch v {
			case "null":
				ty = exprlang.NullType{}
			case "true", "false":
				ty = exprlang.BoolType{}
			default:
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					ty = exprlang.NumberType{}
				}
			}
		case 1:
			if i.Value.IsExpressionAssigned() {
				ty = ts[0].ty
			}
		}

		if !mi.Type.Assignable(ty) {
			rule.Errorf(
				i.Value.Pos,
				"input %q is typed as %s by reusable workflow %q. %s value cannot be assigned",
				mi.Name,
				mi.Type.String(),
				c.Uses.Value,
				ty.String(),
			)
		}
	}

	for _, s := range c.Secrets {
		rule.checkString(s.Value, "jobs.<job_id>.secrets.<secrets_id>")
	}
}

func (rule *RuleExpression) checkSnapshot(s *workflowlang.Snapshot) {
	if s == nil {
		return
	}
	rule.checkWorkflowExpression(s.Expression, "snapshot", "jobs.<job_id>.snapshot", workflowSnapshot)
	for _, field := range []struct {
		value *workflowlang.String
		name  string
	}{{s.ImageName, "image-name"}, {s.Version, "version"}} {
		if field.value != nil && field.value.IsExpressionAssigned() {
			rule.checkWorkflowExpression(field.value, "snapshot."+field.name, "jobs.<job_id>.snapshot", workflowNonEmpty)
		} else {
			rule.checkString(field.value, "jobs.<job_id>.snapshot")
		}
	}
	rule.checkIfCondition(s.If, "jobs.<job_id>.snapshot.if")
}

func (rule *RuleExpression) checkWebhookEventFilter(f *workflowlang.WebhookEventFilter) {
	if f == nil {
		return
	}
	rule.checkStrings(f.Values, "")
}

func (rule *RuleExpression) checkStrings(ss []*workflowlang.String, workflowKey string) {
	for _, s := range ss {
		rule.checkString(s, workflowKey)
	}
}

func (rule *RuleExpression) checkIfCondition(str *workflowlang.String, workflowKey string) {
	if str == nil {
		return
	}

	// Note:
	// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idif
	//
	// > When you use expressions in an if conditional, you may omit the expression syntax (${{ }})
	// > because GitHub automatically evaluates the if conditional as an expression, unless the
	// > expression contains any operators. If the expression contains any operators, the expression
	// > must be contained within ${{ }} to explicitly mark it for evaluation.
	//
	// This document is actually wrong. I confirmed that any strings without surrounding in ${{ }}
	// are evaluated.
	//
	// - run: echo 'run'
	//   if: '!false'
	// - run: echo 'not run'
	//   if: '!true'
	// - run: echo 'run'
	//   if: false || true
	// - run: echo 'run'
	//   if: true && true
	// - run: echo 'not run'
	//   if: true && false

	var condTy exprlang.Type
	restore := rule.scopeExpressionSource(str.Value, str.Pos)
	defer restore()
	expr, err, offset := exprlang.ParseConditionExpression(str.Value)
	if exprlang.ConditionSource(str.Value) != str.Value && offset == 0 {
		rule.expressionSource = nil
	}
	rule.expressionOffset = offset
	line, col := str.Pos.Line, str.Pos.Col+offset
	if offset != 0 && str.Quoted {
		col++
	}
	if err != nil {
		rule.exprError(err, line, col)
		return
	}
	if expr == nil {
		rule.checkString(str, workflowKey)
		return
	}
	if ty, ok := rule.checkSemanticsOfExprNode(expr, line, col, false, workflowKey); ok {
		condTy = ty
	}

	if condTy != nil && !(exprlang.BoolType{}).Assignable(condTy) {
		rule.Errorf(str.Pos, "\"if\" condition should be type \"bool\" but got type %q", condTy.String())
	}
}

func (rule *RuleExpression) checkTemplateEvaluatedType(ts []typedExpr) {
	for _, t := range ts {
		switch t.ty.(type) {
		case *exprlang.ObjectType, *exprlang.ArrayType, exprlang.NullType:
			rule.Errorf(&t.pos, "object, array, and null values should not be evaluated in template with ${{ }} but evaluating the value of type %s", t.ty)
		}
	}
}

func (rule *RuleExpression) checkString(str *workflowlang.String, workflowKey string) []typedExpr {
	if str == nil {
		return nil
	}

	ts, ok := rule.checkExprsIn(str.Value, str.Pos, str.Quoted, false, workflowKey)
	if !ok {
		return nil
	}
	// Contextless schema definitions reject expressions. The runner folds a sole
	// string literal expression into a string, but interpolation remains an expression.
	if workflowKey == "" && len(ts) > 0 && (len(ts) != 1 || !str.IsExpressionAssigned()) {
		rule.Error(str.Pos, "expressions are not allowed in this static workflow field")
		return nil
	}

	rule.checkTemplateEvaluatedType(ts)
	return ts
}

func (rule *RuleExpression) checkScriptString(str *workflowlang.String, workflowKey string) {
	if str == nil {
		return
	}

	ts, ok := rule.checkExprsIn(str.Value, str.Pos, str.Quoted, true, workflowKey)
	if !ok {
		return
	}

	rule.checkTemplateEvaluatedType(ts)
}

func (rule *RuleExpression) checkBool(b *workflowlang.Bool, workflowKey string) {
	if b == nil || b.Expression == nil {
		return
	}

	ty := rule.checkOneExpression(b.Expression, "bool value", workflowKey)
	if ty == nil {
		return
	}

	switch ty.(type) {
	case exprlang.BoolType, exprlang.AnyType:
		// ok
	default:
		rule.Errorf(b.Expression.Pos, "type of expression must be bool but found type %s", ty.String())
	}
}

func (rule *RuleExpression) checkMaxParallel(i *workflowlang.Int, workflowKey string) {
	if i == nil || i.Expression == nil {
		return
	}
	ty := rule.checkOneExpression(i.Expression, "integer value", workflowKey)
	if rule.checkNumberTy(ty, i.Expression.Pos, "integer value") == nil {
		return
	}
	if value, known := workflowlang.WorkflowExpressionLiteral(i.Expression); known {
		for _, message := range workflowExpressionLiteralErrors(workflowPositive, value, "max-parallel") {
			rule.Error(i.Expression.Pos, message)
		}
	}
}

func (rule *RuleExpression) checkFloat(f *workflowlang.Float, workflowKey string) {
	if f == nil {
		return
	}
	rule.checkNumberExpression(f.Expression, "float number value", workflowKey)
}

func (rule *RuleExpression) checkExprsIn(s string, pos *workflowlang.Pos, quoted, checkUntrusted bool, workflowKey string) ([]typedExpr, bool) {
	restore := rule.scopeExpressionSource(s, pos)
	defer restore()

	line, col := pos.Line, pos.Col
	if quoted {
		col++ // when the string is quoted like 'foo' or "foo", column should be incremented
	}
	offset := 0
	ts := []typedExpr{}
	valid := true
	for {
		idx := strings.Index(s, "${{")
		if idx == -1 {
			break
		}

		start := idx + 3 // 3 means removing "${{"
		s = s[start:]
		offset += start
		rule.expressionOffset = offset
		col := col + offset

		ty, offsetAfter, ok := rule.checkSemantics(s, line, col, checkUntrusted, workflowKey)
		// Parsed expressions have a complete boundary even when semantic checks fail.
		// Scan later interpolations for independent rules, but retain aggregate invalidity.
		if !ok && ty == nil {
			return nil, false
		}
		valid = valid && ok
		if ty == nil || offsetAfter == 0 {
			return nil, true
		}
		position := workflowlang.Pos{Line: line, Col: col - 3}
		if mapped := rule.expressionPosition(offset-3, false); mapped != nil {
			position = *mapped
		}
		ts = append(ts, typedExpr{ty, position})

		s = s[offsetAfter:]
		offset += offsetAfter
	}

	return ts, valid
}

func (rule *RuleExpression) exprError(err *exprlang.Error, lineBase, colBase int) {
	pos := convertExprLineColToPos(err.Line, err.Column, lineBase, colBase)
	var end *workflowlang.Pos
	if mapped := rule.expressionPosition(rule.expressionOffset+err.Offset, false); mapped != nil {
		pos = mapped
		end = rule.expressionPosition(rule.expressionOffset+err.EndOffset, true)
	}
	kind := err.Rule
	if kind == "" {
		kind = rule.Name()
	}
	diagnostic := errorAt(pos, kind, err.Message)
	diagnostic.endPosition = end
	rule.errs = append(rule.errs, diagnostic)
}

func (rule *RuleExpression) checkSemanticsOfExprNode(expr exprlang.Node, line, col int, checkUntrusted bool, workflowKey string) (exprlang.Type, bool) {
	c := exprlang.NewChecker(checkUntrusted, expressionOptionsFromConfig(rule.config))
	if rule.matrixTy != nil {
		c.UpdateMatrix(rule.matrixTy)
	}
	if rule.stepsTy != nil {
		c.UpdateSteps(rule.stepsTy)
	}
	if rule.needsTy != nil {
		c.UpdateNeeds(rule.needsTy)
	}
	if rule.secretsTy != nil {
		c.UpdateSecrets(rule.secretsTy)
	}
	if rule.inputsTy != nil {
		c.UpdateInputs(rule.inputsTy)
	}
	if rule.dispatchInputsTy != nil {
		c.UpdateDispatchInputs(rule.dispatchInputsTy)
	}
	if rule.jobsTy != nil {
		c.UpdateJobs(rule.jobsTy)
	}
	if workflowKey != "" {
		ctx, _ := workflowcontext.WorkflowKeyAvailability(workflowKey)
		if len(ctx) == 0 {
			rule.Debug("No context availability was found for workflow key %q", workflowKey)
		}
		c.SetWorkflowKeyAvailability(workflowKey)
	}

	ty, errs := c.Check(expr)
	for _, err := range errs {
		rule.exprError(err, line, col)
	}
	if diagnosticLevel(rule.config, "unsound-ternary") != "off" {
		for _, finding := range exprlang.CheckUnsoundTernaries(expr) {
			rule.exprError(finding, line, col)
		}
	}
	if workflowKey == "" && len(errs) == 0 {
		if _, literal := expr.(*exprlang.StringNode); !literal {
			position := &workflowlang.Pos{Line: line, Col: col}
			if mapped := rule.expressionPosition(rule.expressionOffset, false); mapped != nil {
				position = mapped
			}
			rule.Error(position, "expressions are not allowed in this static workflow field")
			return ty, false
		}
	}

	return ty, len(errs) == 0
}

func (rule *RuleExpression) checkSemantics(src string, line, col int, checkUntrusted bool, workflowKey string) (exprlang.Type, int, bool) {
	l := exprlang.NewLexer(src)
	p := exprlang.NewParser()
	expr, err := p.Parse(l)
	if err != nil {
		rule.exprError(err, line, col)
		return nil, l.Offset(), false
	}
	t, ok := rule.checkSemanticsOfExprNode(expr, line, col, checkUntrusted, workflowKey)
	return t, l.Offset(), ok
}

func (rule *RuleExpression) calcNeedsType(job *workflowlang.Job) *exprlang.ObjectType {
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	o := exprlang.NewEmptyStrictObjectType()
	rule.populateDependantNeedsTypes(o, job, job)
	return o
}

func (rule *RuleExpression) populateDependantNeedsTypes(out *exprlang.ObjectType, job *workflowlang.Job, root *workflowlang.Job) {
	for _, id := range job.Needs {
		i := strings.ToLower(id.Value) // ID is case insensitive
		if i == root.ID.Value {
			continue // When cyclic dependency exists. This does not happen normally.
		}
		if _, ok := out.Props[i]; ok {
			continue // Already added
		}

		j, ok := rule.workflow.Jobs[i]
		if !ok {
			continue
		}

		var outputs *exprlang.ObjectType
		if j.WorkflowCall == nil {
			outputs = exprlang.NewEmptyStrictObjectType()
			for name := range j.Outputs {
				outputs.Props[name] = exprlang.StringType{}
			}
		} else {
			outputs = rule.getWorkflowCallOutputsType(j.WorkflowCall)
		}

		out.Props[i] = exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs": outputs,
			"result":  exprlang.StringType{},
		})

		// Do not collect outputs type from parent of parent recursively. (#151)
	}
}

func (rule *RuleExpression) checkMatrixExpression(expr *workflowlang.String) *exprlang.ObjectType {
	ty := rule.checkObjectExpression(expr, "matrix", "jobs.<job_id>.strategy")
	return matrixTypeFromExpression(ty)
}

func (rule *RuleExpression) checkMatrix(m *workflowlang.Matrix) *exprlang.ObjectType {
	if m.Expression != nil {
		return rule.checkMatrixExpression(m.Expression)
	}

	for _, filters := range []*workflowlang.MatrixCombinations{m.Include, m.Exclude} {
		if filters != nil {
			mapping.CheckSourceKey(filters.Name, filters.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
		}
	}

	// Check types of "exclude" but they are not used to guess type of matrix
	if m.Exclude != nil {
		if m.Exclude.Expression != nil {
			if ty, ok := rule.checkArrayExpression(m.Exclude.Expression, "exclude", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
				rule.checkObjectTy(ty.Elem, m.Exclude.Expression.Pos, "exclude")
			}
		} else {
			for _, combi := range m.Exclude.Combinations {
				if combi.Expression != nil {
					rule.checkMatrixCombinationExpression(combi.Expression, "exclude")
					continue
				}
				for _, a := range combi.Assigns {
					mapping.CheckSourceKey(a.Key, combi.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
					rule.checkRawYAMLValue(a.Value)
				}
			}
		}
	}

	o := exprlang.NewEmptyStrictObjectType()
	for _, insertion := range m.Insertions {
		var inserted *exprlang.ObjectType
		if insertion.Expression != nil {
			ty := rule.checkWorkflowExpression(insertion.Expression, "matrix insertion", "jobs.<job_id>.strategy", workflowStrategy.props["matrix"])
			inserted = matrixTypeFromExpression(ty)
		} else {
			inserted = rule.checkMatrix(insertion)
		}
		if merged, ok := o.Merge(inserted).(*exprlang.ObjectType); ok {
			o = merged
		} else {
			o.Loose()
		}
	}
	for n, r := range m.Rows {
		ty := rule.checkMatrixRow(r)
		if name, known := resolvedMatrixRowName(n, r); known {
			o.Props[name] = ty
		} else {
			o.Loose()
		}
	}

	if m.Include == nil {
		return o
	}

	if m.Include.Expression != nil {
		if a, ok := rule.checkOneExpression(m.Include.Expression, "include", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			if ret, ok := o.Merge(a.Elem).(*exprlang.ObjectType); ok {
				return ret
			}
		}
		return exprlang.NewEmptyObjectType()
	}

	for _, combi := range m.Include.Combinations {
		if combi.Expression != nil {
			ty := rule.checkMatrixCombinationExpression(combi.Expression, "include")
			if ty == nil {
				continue
			}
			if merged, ok := o.Merge(ty).(*exprlang.ObjectType); ok {
				o = merged
			} else {
				o.Loose()
			}
			continue
		}

		for n, assign := range combi.Assigns {
			mapping.CheckSourceKey(assign.Key, combi.Evaluated, func(source *workflowlang.String) { rule.checkString(source, "jobs.<job_id>.strategy") })
			key := workflowlang.InputResolver.ResolveKey(assign.Key, combi.Evaluated)
			if key.State == mapping.KnownKey {
				n = strings.ToLower(key.Name.Value)
			} else {
				o.Loose()
			}
			ty := rule.checkRawYAMLValue(assign.Value)
			if t, ok := o.Props[n]; ok {
				// When the combination exists in 'matrix' section, merge type with existing one
				ty = t.Merge(ty)
			}
			if key.State == mapping.KnownKey {
				o.Props[n] = ty
			}
		}
	}

	return o
}

func (rule *RuleExpression) checkMatrixRow(r *workflowlang.MatrixRow) exprlang.Type {
	mapping.CheckSourceKey(r.Name, r.Evaluated, func(source *workflowlang.String) {
		rule.checkString(source, "jobs.<job_id>.strategy")
	})
	if r.Expression != nil {
		if a, ok := rule.checkArrayExpression(r.Expression, "matrix row", "jobs.<job_id>.strategy").(*exprlang.ArrayType); ok {
			return a.Elem
		}
		return exprlang.AnyType{}
	}

	var ty exprlang.Type
	for _, v := range r.Values {
		t := rule.checkMatrixSequenceItem(v)
		if t == nil {
			continue
		}
		if ty == nil {
			ty = t
		} else {
			ty = ty.Merge(t)
		}
	}

	if ty == nil {
		return exprlang.AnyType{} // No element
	}

	return ty
}

func (rule *RuleExpression) checkMatrixCombinationExpression(s *workflowlang.String, section string) exprlang.Type {
	ty := rule.checkOneExpression(s, "matrix combination at element of "+section+" section", "jobs.<job_id>.strategy")
	if array, ok := ty.(*exprlang.ArrayType); ok {
		if value, known := workflowlang.WorkflowExpressionLiteral(s); known {
			if values, ok := value.([]any); ok {
				// Merging object and scalar element types produces AnyType, so
				// validate each known value before using the inferred element type.
				invalid := false
				for i, value := range values {
					if _, ok := value.(map[string]any); !ok {
						rule.Errorf(s.Pos, "matrix combination in %s at inserted array index %d must be an object but got %s", section, i, exprlang.TypeOfJSONValue(value))
						invalid = true
					}
				}
				if invalid || len(values) == 0 {
					return nil
				}
			}
		}
		ty = array.Elem
	}
	switch ty.(type) {
	case nil, exprlang.AnyType, *exprlang.ObjectType:
		return ty
	default:
		rule.Errorf(s.Pos, "matrix combination in %s must be an object or an array of objects but got %s", section, ty)
		return nil
	}
}

func (rule *RuleExpression) checkMatrixSequenceItem(value workflowlang.RawYAMLValue) exprlang.Type {
	ty := rule.checkRawYAMLValue(value)
	if scalar, ok := value.(*workflowlang.RawYAMLString); ok && exprlang.ParseAssignedExpression(scalar.Value) != nil {
		if array, ok := ty.(*exprlang.ArrayType); ok {
			if literal, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: scalar.Value}); known {
				if values, ok := literal.([]any); ok && len(values) == 0 {
					return nil
				}
			}
			return array.Elem // Template expansion inserts one level only.
		}
	}
	return ty
}

func (rule *RuleExpression) checkWorkflowCallOutputs(outputs map[string]*workflowlang.WorkflowCallEventOutput, jobs map[string]*workflowlang.Job) {
	if len(outputs) == 0 || len(jobs) == 0 {
		return
	}

	props := make(map[string]exprlang.Type, len(jobs))
	for n, j := range jobs {
		var o *exprlang.ObjectType
		if j.WorkflowCall != nil {
			// Outputs are not defined in jobs.<job_id> section when it is reusable workflow call.
			o = exprlang.NewEmptyObjectType()
		} else {
			p := make(map[string]exprlang.Type, len(j.Outputs))
			for n := range j.Outputs {
				p[n] = exprlang.StringType{}
			}
			o = exprlang.NewStrictObjectType(p)
		}
		props[n] = exprlang.NewStrictObjectType(map[string]exprlang.Type{
			"outputs": o,
			"result":  exprlang.StringType{},
		})
	}
	rule.jobsTy = exprlang.NewStrictObjectType(props)

	for _, o := range outputs {
		rule.checkString(o.Value, "on.workflow_call.outputs.<output_id>.value")
	}
}

func (rule *RuleExpression) checkRawYAMLValue(v workflowlang.RawYAMLValue) exprlang.Type {
	switch v := v.(type) {
	case *workflowlang.RawYAMLObject:
		m := make(map[string]exprlang.Type, len(v.Props))
		for k, p := range v.Props {
			m[k] = rule.checkRawYAMLValue(p)
		}
		return exprlang.NewStrictObjectType(m)
	case *workflowlang.RawYAMLArray:
		var elem exprlang.Type
		for _, value := range v.Elems {
			if ty := rule.checkMatrixSequenceItem(value); ty != nil {
				if elem == nil {
					elem = ty
				} else {
					elem = elem.Merge(ty)
				}
			}
		}
		if elem == nil {
			elem = exprlang.AnyType{}
		}
		return &exprlang.ArrayType{Elem: elem, Deref: false}
	case *workflowlang.RawYAMLString:
		return rule.checkRawYAMLString(v)
	default:
		panic("unreachable")
	}
}

func (rule *RuleExpression) checkRawYAMLString(y *workflowlang.RawYAMLString) exprlang.Type {
	ts, ok := rule.checkExprsIn(y.Value, y.Pos(), false, false, "jobs.<job_id>.strategy")

	if workflowlang.IsExprAssigned(y.Value) {
		if !ok || len(ts) != 1 {
			return exprlang.AnyType{}
		}
		if value, known := workflowlang.WorkflowExpressionLiteral(&workflowlang.String{Value: y.Value}); known {
			return exprlang.TypeOfJSONValue(value)
		}
		return ts[0].ty
	}

	switch y.Tag {
	case workflowlang.YamlTagBool:
		if yamlscalar.IsCoreSchemaBool(y.Value) {
			return exprlang.BoolType{}
		}
	case workflowlang.YamlTagNull:
		if yamlscalar.IsCoreSchemaNull(y.Value) {
			return exprlang.NullType{}
		}
	case workflowlang.YamlTagInt, workflowlang.YamlTagFloat:
		if yamlscalar.IsCoreSchemaNumber(y.Value) {
			return exprlang.NumberType{}
		}
	}
	return exprlang.StringType{}
}

func convertExprLineColToPos(line, col, lineBase, colBase int) *workflowlang.Pos {
	// Line and column in ExprError are 1-based
	return &workflowlang.Pos{
		Line: line - 1 + lineBase,
		Col:  col - 1 + colBase,
	}
}

func typeOfActionOutputs(meta *ActionMetadata) *exprlang.ObjectType {
	// Some action sets outputs dynamically. Such outputs are not defined in action.yml. actionlint
	// cannot check such outputs statically so it allows any props (#18)
	if meta.SkipOutputs {
		return exprlang.NewEmptyObjectType()
	}
	props := make(map[string]exprlang.Type, len(meta.Outputs))
	for n := range meta.Outputs {
		props[strings.ToLower(n)] = exprlang.StringType{}
	}
	return exprlang.NewStrictObjectType(props)
}

func expressionOptionsFromConfig(config *Config) *exprlang.Options {
	options := &exprlang.Options{Checks: exprlang.Checks{
		StringConditions:          diagnosticLevel(config, "string-conditions") != "off",
		MixedTypeComparisons:      diagnosticLevel(config, "mixed-type-comparisons") != "off",
		CaseInsensitiveConditions: diagnosticLevel(config, "case-insensitive-conditions") != "off",
	}}
	if config != nil {
		options.Variables, options.Secrets = config.ConfigVariables, config.ConfigSecrets
	}
	return options
}
