package workflow

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	exprlang "actionlint.kjanat.dev/v2/internal/expression"
	"actionlint.kjanat.dev/v2/internal/yaml"
	yamlscalar "actionlint.kjanat.dev/v2/internal/yamlscalar"
	"actionlint.kjanat.dev/v2/internal/yamlsource"
)

// Pos represents a position in the file.
type Pos struct {
	// Line is a line number of the position. This value is 1-based.
	Line int
	// Col is a column number of the position. This value is 1-based.
	Col int
}

type scriptSourceLine struct {
	Start int
	End   int
}

type ScriptSourceSpan struct {
	Start      int
	End        int
	YamlLine   int
	YamlColumn int
	// Nonzero for a decoded escape whose source width differs from its value.
	YamlEndColumn int
	YamlEndLine   int
}

// ScriptSource maps spans in a decoded run script back to positions in the YAML source. Bytes that
// were synthesized while decoding YAML, such as whitespace between folded plain-scalar lines, are
// intentionally left outside the mapped spans.
type ScriptSource struct {
	ContentStart Pos
	ScalarEnd    Pos
	Value        string
	Lines        []scriptSourceLine
	Spans        []ScriptSourceSpan
	Literal      bool
}

func newScriptSource(value string) *ScriptSource {
	s := &ScriptSource{Value: value}
	start := 0
	for {
		i := strings.IndexByte(value[start:], '\n')
		if i < 0 {
			s.Lines = append(s.Lines, scriptSourceLine{Start: start, End: len(value)})
			return s
		}
		end := start + i
		s.Lines = append(s.Lines, scriptSourceLine{Start: start, End: end})
		start = end + 1
	}
}

func (s *ScriptSource) Position(line, col int) (*Pos, bool) {
	return s.lookup(line, col, false)
}

func (s *ScriptSource) EndPos(line, col int) (*Pos, bool) {
	return s.lookup(line, col, true)
}

func (s *ScriptSource) lookup(line, col int, endPosition bool) (*Pos, bool) {
	if s == nil || line <= 0 || line > len(s.Lines) || col <= 0 {
		return nil, false
	}
	l := s.Lines[line-1]
	rel, ok := yamlsource.ByteOffsetAtColumn(s.Value[l.Start:l.End], col)
	if !ok {
		return nil, false
	}
	offset := l.Start + rel
	// Block chomping and quoted folding can append unmapped newlines or spaces.
	// An EOF diagnostic belongs at the end of the final mapped physical line.
	if endPosition && offset == len(s.Value) && len(s.Spans) != 0 {
		index := len(s.Spans) - 1
		for index > 0 && s.Spans[index].Start == s.Spans[index].End {
			index--
		}
		last := s.Spans[index]
		if last.End < offset && strings.Trim(s.Value[last.End:], " \n") == "" {
			column := last.YamlColumn + utf8.RuneCountInString(s.Value[last.Start:last.End])
			if last.YamlEndColumn != 0 {
				column = last.YamlEndColumn
			}
			line := last.YamlLine
			if last.YamlEndLine != 0 {
				line = last.YamlEndLine
			}
			return &Pos{Line: line, Col: column}, true
		}
	}
	i := sort.Search(len(s.Spans), func(i int) bool {
		if endPosition {
			return s.Spans[i].End >= offset
		}
		return s.Spans[i].End > offset
	})
	if i == len(s.Spans) {
		return nil, false
	}
	span := s.Spans[i]
	if offset < span.Start || (endPosition && offset == span.Start) {
		return nil, false
	}
	column := span.YamlColumn + utf8.RuneCountInString(s.Value[span.Start:offset])
	if span.YamlEndColumn != 0 {
		column = span.YamlColumn
		if endPosition && offset == span.End {
			column = span.YamlEndColumn
		}
	}
	line = span.YamlLine
	if endPosition && offset == span.End && span.YamlEndLine != 0 {
		line = span.YamlEndLine
	}
	return &Pos{Line: line, Col: column}, true
}

func (p *Pos) String() string {
	return fmt.Sprintf("line:%d,col:%d", p.Line, p.Col)
}

// IsBefore returns if the Position is before the other Position. If they are equal, this function returns false.
func (p *Pos) IsBefore(other *Pos) bool {
	if p.Line < other.Line {
		return true
	}
	if p.Line > other.Line {
		return false
	}
	return p.Col < other.Col
}

// String represents generic String value in YAML file with Position.
type String struct {
	// Value is a raw value of the String.
	Value string
	// Quoted represents the String is quoted with ' or " in the YAML source.
	Quoted bool
	// Pos is a Position of the String in source.
	Pos *Pos
}

// ContainsExpression returns whether the String contains at least one ${{ }} expression.
func (s *String) ContainsExpression() bool {
	return exprlang.ContainsExpression(s.Value)
}

func IsExprAssigned(s string) bool {
	if !strings.HasPrefix(s, "${{") || !strings.HasSuffix(s, "}}") {
		return false
	}
	// Delimiters inside single-quoted strings are data. Doubled quotes toggle
	// twice, keeping the scan inside the same String literal.
	inString := false
	for i := 3; i < len(s)-1; i++ {
		if s[i] == '\'' {
			inString = !inString
		} else if !inString {
			if strings.HasPrefix(s[i:], "}}") {
				return i+2 == len(s)
			}
			if strings.HasPrefix(s[i:], "${{") {
				return false
			}
		}
	}
	// Let the expression parser diagnose an unterminated String literal.
	return inString
}

// IsExpressionAssigned returns whether a single expression is assigned to the String.
func (s *String) IsExpressionAssigned() bool {
	return IsExprAssigned(s.Value)
}

// Bool represents generic boolean value in YAML file with Position.
type Bool struct {
	// Value is a raw value of the Bool String.
	Value bool
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

func (b *Bool) String() string {
	if b.Expression != nil {
		return b.Expression.Value
	}
	if b.Value {
		return "true"
	}
	return "false"
}

// Int represents generic integer value in YAML file with Position.
type Int struct {
	// Value is a raw value of the integer String.
	Value int
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

// Float represents generic Float value in YAML file with Position.
type Float struct {
	// Value is a raw value of the Float String.
	Value float64
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

// Event interface represents Workflow events in 'on' section
type Event interface {
	// EventName returns name of the Event to trigger this Workflow.
	EventName() string
}

// WebhookEventFilter is a filter for Webhook events such as 'branches', 'paths-ignore', ...
// Webhook events are filtered by those filters. Some filters are exclusive.
// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#using-filters
type WebhookEventFilter struct {
	// Name is a name of filter such like 'branches', 'tags'
	Name *String
	// Values is a list of filter values.
	Values []*String
}

// IsEmpty returns true when it has no value. This may mean the WebhookEventFilter instance itself is nil.
func (f *WebhookEventFilter) IsEmpty() bool {
	return f == nil || len(f.Values) == 0
}

// WebhookEvent represents Event type based on webhook events.
// Some events can't have 'types' field. Only 'push' and 'pull' events can have 'tags', 'tags-ignore',
// 'paths' and 'paths-ignore' fields. Only 'workflow_run' Event can have 'workflows' field.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onevent_nametypes
type WebhookEvent struct {
	// Hook is a name of the webhook Event.
	Hook *String
	// Types is list of types of the webhook Event. Only the types enumerated here will trigger
	// the Workflow.
	Types []*String
	// Branches is 'branches' filter. This value is nil when it is omitted.
	Branches *WebhookEventFilter
	// BranchesIgnore is 'branches-ignore' filter. This value is nil when it is omitted.
	BranchesIgnore *WebhookEventFilter
	// Tags is 'tags' filter. This value is nil when it is omitted.
	Tags *WebhookEventFilter
	// TagsIgnore is 'tags-ignore' filter. This value is nil when it is omitted.
	TagsIgnore *WebhookEventFilter
	// Paths is 'paths' filter. This value is nil when it is omitted.
	Paths *WebhookEventFilter
	// PathsIgnore is 'paths-ignore' filter. This value is nil when it is omitted.
	PathsIgnore *WebhookEventFilter
	// Workflows is list of Workflow names which are triggered by 'workflow_run' Event.
	Workflows []*String
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *WebhookEvent) EventName() string {
	return e.Hook.Value
}

// ScheduleEntry is a single entry in a schedule Event.
// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#onschedule
type ScheduleEntry struct {
	// Cron is the cron String for the schedule.
	Cron *String
	// Timezone is the optional IANA timezone for the schedule.
	Timezone *String
}

// ScheduledEvent is Event scheduled by Workflow.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#scheduled-events
type ScheduledEvent struct {
	// Schedules is list of schedule entries which schedule the Workflow.
	Schedules []*ScheduleEntry
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *ScheduledEvent) EventName() string {
	return "schedule"
}

// WorkflowDispatchEventInputType is a type for Input types of workflow_dispatch events.
// https://github.blog/changelog/2021-11-10-github-actions-Input-types-for-manual-workflows/
type WorkflowDispatchEventInputType uint8

const (
	// WorkflowDispatchEventInputTypeNone represents no type is specified to the Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeNone WorkflowDispatchEventInputType = iota
	// WorkflowDispatchEventInputTypeString is String type of Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeString
	// WorkflowDispatchEventInputTypeNumber is number type of Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeNumber
	// WorkflowDispatchEventInputTypeBoolean is boolean type of Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeBoolean
	// WorkflowDispatchEventInputTypeChoice is choice type of Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeChoice
	// WorkflowDispatchEventInputTypeEnvironment is Environment type of Input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeEnvironment
)

// DispatchInput is Input specified on dispatching Workflow manually.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type DispatchInput struct {
	// Name is a name of Input value specified on dispatching Workflow manually.
	Name *String
	// Description is a description of Input value specified on dispatching Workflow manually.
	Description *String
	// Required is a flag to show if this Input is mandatory or not on dispatching Workflow manually.
	Required *Bool
	// Default is a default value of Input value on dispatching Workflow manually.
	Default *String
	// Type is a type of the Input
	// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
	Type WorkflowDispatchEventInputType
	// Options is list of options of choice type
	Options []*String
}

// WorkflowDispatchEvent is Event on dispatching Workflow manually.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type WorkflowDispatchEvent struct {
	// Inputs is map from Input names to Input attributes. Keys are in lower case since they are case insensitive.
	Inputs map[string]*DispatchInput
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *WorkflowDispatchEvent) EventName() string {
	return "workflow_dispatch"
}

// RepositoryDispatchEvent is repository_dispatch Event configuration.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#repository_dispatch
type RepositoryDispatchEvent struct {
	// Types is list of types which can trigger Workflow.
	Types []*String
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *RepositoryDispatchEvent) EventName() string {
	return "repository_dispatch"
}

// WorkflowCallEventInputType is a type of inputs at workflow_call Event.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinput_idtype
type WorkflowCallEventInputType uint8

const (
	// WorkflowCallEventInputTypeInvalid represents invalid type Input as default value of the type.
	WorkflowCallEventInputTypeInvalid WorkflowCallEventInputType = iota
	// WorkflowCallEventInputTypeBoolean represents boolean type Input.
	WorkflowCallEventInputTypeBoolean
	// WorkflowCallEventInputTypeNumber represents number type Input.
	WorkflowCallEventInputTypeNumber
	// WorkflowCallEventInputTypeString represents String type Input.
	WorkflowCallEventInputTypeString
)

// WorkflowCallEventInput is an Input configuration of workflow_call Event.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinputs
type WorkflowCallEventInput struct {
	// Name is a name of the Input.
	Name *String
	// Description is a description of the Input.
	Description *String
	// Default is a default value of the Input. Nil means no default value.
	Default *String
	// Required represents if the Input is required or optional. When this value is nil, it was not explicitly specified.
	// In the case the default value is 'not required'.
	Required *Bool
	// Type of the Input, which must be one of 'boolean', 'number' or 'String'. This property is required.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinput_idtype
	Type WorkflowCallEventInputType
	// ID is an ID of the Input. Input ID is in lower case because it is case-insensitive.
	ID string
}

// IsRequired returns if the Input is marked as required or not.
// require
func (i *WorkflowCallEventInput) IsRequired() bool {
	return i.Required != nil && i.Required.Value
}

// WorkflowCallEventSecret is a secret configuration of workflow_call Event.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecrets
type WorkflowCallEventSecret struct {
	// Name is a name of the secret.
	Name *String
	// Description is a description of the secret.
	Description *String
	// Required represents if the secret is required or optional. When this value is nil, it means optional.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecretssecret_idrequired
	Required *Bool
}

// WorkflowCallEventOutput is an Output configuration of workflow_call Event.
// https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-Workflow
type WorkflowCallEventOutput struct {
	// Name is a name of the Output.
	Name *String
	// Description is a description of the Output.
	Description *String
	// Value is an expression for the value of the Output.
	Value *String
}

// WorkflowCallEvent is workflow_call Event configuration.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#Workflow-reuse-events
type WorkflowCallEvent struct {
	// Inputs is an array of inputs of the workflow_call Event. This value is not a map unlike other fields of this
	// struct since its order is important when checking the default values of inputs.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinputs
	Inputs []*WorkflowCallEventInput
	// Secrets is a map from name of secret to secret configuration. When 'secrets' is omitted, nil is set to this
	// field. Keys are in lower case since they are case-insensitive.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecrets
	Secrets map[string]*WorkflowCallEventSecret
	// Outputs is a map from name of Output to Output configuration. Keys are in lower case since they are case-insensitive.
	// https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-Workflow
	Outputs map[string]*WorkflowCallEventOutput
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *WorkflowCallEvent) EventName() string {
	return "workflow_call"
}

// ImageVersionEvent is image_version Event configuration.
// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#image_version
type ImageVersionEvent struct {
	// Types filters image version activity to created, ready, or deleted events.
	Types []*String
	// Names is a list of names which match to the created image names.
	Names []*String
	// Versions is a list of names which match to the created image versions. Glob patterns are available.
	Versions []*String
	// Pos is a Position in source.
	Pos *Pos
}

// EventName returns name of the Event to trigger this Workflow.
func (e *ImageVersionEvent) EventName() string {
	return "image_version"
}

// PermissionScope is struct for respective permission scope like "issues", "checks", ...
// https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication#Permissions-for-the-github_token
type PermissionScope struct {
	// Name is name of the scope.
	Name *String
	// Value is permission value of the scope.
	Value *String
}

// Permissions is set of permission configurations in Workflow file. All Permissions can be set at
// once. Or each permission can be configured respectively.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Permissions
type Permissions struct {
	// All represents a permission value for all the scopes at once.
	All *String
	// Scopes is mappings from scope name to its permission configuration
	Scopes map[string]*PermissionScope
	// Pos is a Position in source.
	Pos *Pos
}

// DefaultsRun is configuration that shell is how to be run.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#defaultsrun
type DefaultsRun struct {
	// Expression supplies the complete run Defaults mapping.
	Expression *String
	// Shell is shell name to be run.
	Shell *String
	// WorkingDirectory is a default working directory path.
	WorkingDirectory *String
	// Pos is a Position in source.
	Pos *Pos
}

// Defaults is set of default configurations to run shell.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Defaults
type Defaults struct {
	// Run is configuration of how to run shell.
	Run *DefaultsRun
	// Pos is a Position in source.
	Pos *Pos
}

// Concurrency is a configuration of Concurrency of the Workflow.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Concurrency
type Concurrency struct {
	// Expression supplies a group String or complete Concurrency mapping.
	Expression *String
	// Group is name of the Concurrency group.
	Group *String
	// CancelInProgress is a flag that shows if canceling this Workflow cancels other jobs in progress.
	CancelInProgress *Bool
	// Queue is the queue Strategy for pending Workflow runs. Valid values are "single" (default) and "max".
	Queue *String
	// Pos is a Position in source.
	Pos *Pos
}

// Environment is a configuration of Environment.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idenvironment
type Environment struct {
	// Expression supplies an Environment name or complete Environment mapping.
	Expression *String
	// Name is a name of Environment which the Workflow uses.
	Name *String
	// URL is the URL mapped to 'environment_url' in the deployments API. Empty value means no value was specified.
	URL *String
	// Deployment is whether GitHub should create a deployment for this Job. false skips auto-deployment while still applying Environment protection rules and secrets.
	Deployment *Bool
	// Pos is a Position in source.
	Pos *Pos
}

// ExecKind is Kind of how the Step is executed. A Step runs some action or runs some shell script.
type ExecKind uint8

const (
	// ExecKindAction is Kind for Step to run action
	ExecKindAction ExecKind = iota
	// ExecKindRun is Kind for Step to run shell script
	ExecKindRun
	// ExecKindWait is Kind for Step to wait for background steps ('wait' or 'wait-all')
	ExecKindWait
	// ExecKindCancel is Kind for Step to cancel background steps ('cancel')
	ExecKindCancel
	// ExecKindParallel is Kind for Step to run a group of steps in parallel ('parallel')
	ExecKindParallel
)

// Exec is an interface how the Step is executed. Step in Workflow runs either an action or a script
type Exec interface {
	// Kind returns Kind of the Step execution.
	Kind() ExecKind
}

// ExecRun is configuration how to run shell script at the Step.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsrun
type ExecRun struct {
	// Run is script to run.
	Run *String
	// source maps positions in Run.Value back to the original YAML source when this can be done
	// exactly. It is intentionally kept internal because it is parser metadata rather than syntax.
	Source *ScriptSource
	// Shell represents optional 'shell' field. Nil means nothing specified.
	Shell *String
	// WorkingDirectory represents optional 'working-directory' field. Nil means nothing specified.
	WorkingDirectory *String
	// RunPos is Position of 'run' section
	RunPos *Pos
}

// Kind returns Kind of the Step execution.
func (e *ExecRun) Kind() ExecKind {
	return ExecKindRun
}

// Input is an Input field for running an action.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswith
type Input struct {
	// Name is a name of the Input.
	Name *String
	// Value is a value of the Input.
	Value *String
	// Source maps the decoded input back to its declaring YAML scalar.
	Source *ScriptSource
}

// ActionInputMapping is a literal or expression-valued action Input mapping.
type ActionInputMapping struct {
	// Inputs contains named Input values.
	Inputs     map[string]*Input
	Expression *String
	// Insertions contains nested mapping directives.
	Insertions []*ActionInputMapping
}

// ExecAction is configuration how to run action at the Step.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsuses
type ExecAction struct {
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsuses
	// Uses is nil when its value could not be parsed as a nonempty String.
	Uses *String
	// Inputs represents inputs to the action to execute in 'with' section. Keys are in lower case since they are case-insensitive.
	Inputs map[string]*Input
	// InputsExpression supplies the complete action inputs mapping.
	InputsExpression *String
	// InputInsertions supplies additional action inputs through mapping directives.
	InputInsertions []*ActionInputMapping
	// Entrypoint represents optional 'entrypoint' field in 'with' section. Nil field means nothing specified
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswithentrypoint
	Entrypoint *String
	// Args represents optional 'args' field in 'with' section. Nil field means nothing specified
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswithargs
	Args *String

	// Constant mapping results must not be interpreted as expression source.
	EntrypointEvaluated  bool
	ArgsEvaluated        bool
	InputNameExpressions []*String
}

// Kind returns Kind of the Step execution.
func (e *ExecAction) Kind() ExecKind {
	return ExecKindAction
}

// ExecWait is configuration of a Step that waits for background steps to complete. It corresponds to
// the 'wait' and 'wait-all' steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecWait struct {
	// Names is the list of background Step IDs to wait for, given by the 'wait' field. It is nil when
	// 'wait-all' is used instead.
	Names []*String
	// All is true when the Step waits for all preceding background steps via the 'wait-all' field.
	All bool
	// AllPos is the Position of the 'wait-all' field. It is nil when 'wait' is used instead.
	AllPos *Pos
}

// Kind returns Kind of the Step execution.
func (e *ExecWait) Kind() ExecKind {
	return ExecKindWait
}

// ExecCancel is configuration of a Step that cancels a running background Step. It corresponds to the
// 'cancel' Step.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecCancel struct {
	// Name is the ID of the background Step to cancel, given by the 'cancel' field. The 'cancel' Step
	// targets a single background Step by its ID.
	Name *String
}

// Kind returns Kind of the Step execution.
func (e *ExecCancel) Kind() ExecKind {
	return ExecKindCancel
}

// ExecParallel is configuration of a Step that runs a group of steps in parallel. It corresponds to
// the 'parallel' Step.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecParallel struct {
	// Steps is the group of steps to run in parallel, given by the 'parallel' field.
	Steps []*Step
}

// Kind returns Kind of the Step execution.
func (e *ExecParallel) Kind() ExecKind {
	return ExecKindParallel
}

// RawYAMLValueKind is Kind of raw YAML values
type RawYAMLValueKind int

const (
	// RawYAMLValueKindObject is Kind for an object value of raw YAML value.
	RawYAMLValueKindObject = RawYAMLValueKind(yaml.MappingNode)
	// RawYAMLValueKindArray is Kind for an array value of raw YAML value.
	RawYAMLValueKindArray = RawYAMLValueKind(yaml.SequenceNode)
	// RawYAMLValueKindString is Kind for a String value of raw YAML value.
	RawYAMLValueKindString = RawYAMLValueKind(yaml.ScalarNode)
)

// RawYAMLValue is a value at Matrix variation. Any value can be put at Matrix variations
// including mappings and arrays.
type RawYAMLValue interface {
	// Kind returns Kind of raw YAML value.
	Kind() RawYAMLValueKind
	// Equals returns if the other value is equal to the value.
	Equals(other RawYAMLValue) bool
	// Pos returns the start Position of the value in the source file
	Pos() *Pos
	// String returns String representation of the value
	String() string
}

// RawYAMLObject is raw YAML mapping value.
type RawYAMLObject struct {
	// Props is map from property names to their values. Keys are in lower case since they are case-insensitive.
	Props    map[string]RawYAMLValue
	Position *Pos
}

// Kind returns Kind of raw YAML value.
func (o *RawYAMLObject) Kind() RawYAMLValueKind {
	return RawYAMLValueKindObject
}

// Equals returns if the other value is equal to the value.
func (o *RawYAMLObject) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLObject:
		if len(o.Props) != len(other.Props) {
			return false
		}
		for n, p1 := range o.Props {
			if p2, ok := other.Props[n]; !ok || !p1.Equals(p2) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Pos returns the start Position of the value in the source file
func (o *RawYAMLObject) Pos() *Pos {
	return o.Position
}

func (o *RawYAMLObject) String() string {
	qs := make([]string, 0, len(o.Props))
	for n, p := range o.Props {
		qs = append(qs, fmt.Sprintf("%q: %s", n, p.String()))
	}
	sort.Strings(qs)
	return "{" + strings.Join(qs, ", ") + "}"
}

// RawYAMLArray is raw YAML sequence value.
type RawYAMLArray struct {
	// Elems is list of elements of the array value.
	Elems    []RawYAMLValue
	Position *Pos
}

// Kind returns Kind of raw YAML value.
func (a *RawYAMLArray) Kind() RawYAMLValueKind {
	return RawYAMLValueKindArray
}

// Equals returns if the other value is equal to the value.
func (a *RawYAMLArray) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLArray:
		if len(a.Elems) != len(other.Elems) {
			return false
		}
		for i, e1 := range a.Elems {
			if !e1.Equals(other.Elems[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Pos returns the start Position of the value in the source file
func (a *RawYAMLArray) Pos() *Pos {
	return a.Position
}

func (a *RawYAMLArray) String() string {
	var b strings.Builder
	b.WriteRune('[')

	comma := false
	for _, e := range a.Elems {
		if comma {
			b.WriteString(", ")
		} else {
			comma = true
		}
		b.WriteString(e.String())
	}

	b.WriteRune(']')
	return b.String()
}

// RawYAMLString is raw YAML scalar value.
type RawYAMLString struct {
	// Value is String representation of the scalar node.
	Value string
	// Tag is the YAML tag of the scalar node, such as "!!str" or "!!Int". A tag the parser rejects
	// is recorded as "!!str".
	Tag      string
	Position *Pos
}

// Kind returns Kind of raw YAML value.
func (s *RawYAMLString) Kind() RawYAMLValueKind {
	return RawYAMLValueKindString
}

// Equals returns if the other value is equal to the value.
func (s *RawYAMLString) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLString:
		a, b := s.ScalarValue(), other.ScalarValue()
		if a == b {
			return true
		}
		x, xok := a.(float64)
		y, yok := b.(float64)
		return xok && yok && math.IsNaN(x) && math.IsNaN(y)
	default:
		return false
	}
}

func (s *RawYAMLString) ScalarValue() any {
	switch s.Tag {
	case YamlTagBool:
		if yamlscalar.IsCoreSchemaBool(s.Value) {
			return strings.EqualFold(s.Value, "true")
		}
	case YamlTagNull:
		if yamlscalar.IsCoreSchemaNull(s.Value) {
			return nil
		}
	case YamlTagInt, YamlTagFloat:
		if !yamlscalar.IsCoreSchemaNumber(s.Value) {
			break
		}
		switch strings.ToLower(s.Value) {
		case ".inf", "+.inf":
			return math.Inf(1)
		case "-.inf":
			return math.Inf(-1)
		case ".nan":
			return math.NaN()
		}
		if strings.HasPrefix(s.Value, "0x") || strings.HasPrefix(s.Value, "0o") {
			// GitHub converts hexadecimal and octal scalars through a signed 32-bit integer.
			if n, err := strconv.ParseUint(s.Value, 0, 32); err == nil {
				if n > math.MaxInt32 {
					return float64(n) - (1 << 32)
				}
				return float64(n)
			}
		} else if n, err := strconv.ParseFloat(s.Value, 64); err == nil {
			return n
		}
	}
	return s.Value
}

// Pos returns the start Position of the value in the source file
func (s *RawYAMLString) Pos() *Pos {
	return s.Position
}

func (s *RawYAMLString) String() string {
	if _, ok := s.ScalarValue().(string); ok {
		return strconv.Quote(s.Value)
	}
	if s.Value == "" {
		return "null"
	}
	return s.Value
}

// MatrixRow is one row of Matrix. One Matrix row can take multiple values. Those variations are
// stored as row of values in this struct.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixRow struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	// Name is a name of Matrix value.
	Name *String
	// Values is variations of values which the Matrix value can take.
	Values []RawYAMLValue
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// MatrixAssign represents which value should be taken in the row of the Matrix.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixAssign struct {
	// Key is a name of the Matrix value.
	Key *String
	// Value is the value selected from values in row.
	Value RawYAMLValue
}

// MatrixCombination is combination of Matrix value assignments to define one of Matrix variations.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixCombination struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	Assigns   map[string]*MatrixAssign
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// MatrixCombinations is list of combinations of Matrix assignments used for 'include' and 'exclude'
// sections.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixCombinations struct {
	// Name retains the original include/exclude key for expression validation.
	Name         *String
	Evaluated    bool
	Combinations []*MatrixCombination
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// ContainsExpression returns if the combinations section includes at least one expression node.
func (cs *MatrixCombinations) ContainsExpression() bool {
	if cs.Expression != nil {
		return true
	}
	for _, c := range cs.Combinations {
		if c.Expression != nil {
			return true
		}
	}
	return false
}

// Matrix is Matrix variations configuration of a Job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type Matrix struct {
	// Insertions supplies additional axes or filters through mapping directives.
	Insertions []*Matrix
	// Values stores mappings from name to values. Keys are in lower case since they are case-insensitive.
	Rows map[string]*MatrixRow
	// Include is list of combinations of Matrix values and additional values on running Matrix combinations.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#example-including-additional-values-into-combinations
	Include *MatrixCombinations
	// Exclude is list of combinations of Matrix values which should not be run. Combinations in
	// this list will be removed from combinations of Matrix to run.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#example-excluding-configurations-from-a-Matrix
	Exclude *MatrixCombinations
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

// Strategy is Strategy configuration of how the Job is run.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategy
type Strategy struct {
	// Expression supplies the complete Strategy mapping.
	Expression *String
	// Matrix is Matrix of combinations of values. Each combination will run the Job once.
	Matrix *Matrix
	// FailFast is flag to show if other jobs should stop when one Job fails.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategyfail-fast
	FailFast *Bool
	// MaxParallel is how many jobs should be run at once.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymax-parallel
	MaxParallel *Int
	// Pos is a Position in source.
	Pos *Pos
}

// EnvVar represents key-value of Environment variable setup.
type EnvVar struct {
	// Name is name of the Environment variable.
	Name *String
	// Value is String value of the Environment variable.
	Value *String
}

// Env represents set of Environment variables.
type Env struct {
	// Vars is mapping from Env var name to Env var value.
	Vars map[string]*EnvVar
	// Insertions contains Environment mappings or object expressions supplied by ${{ insert }} directives.
	Insertions []*Env
	// Expression is an expression String which contains ${{ ... }}. When this value is not empty,
	// Vars and Insertions should be nil.
	Expression *String
}

// Step is Step configuration. Step runs one action or one shell script.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idsteps
type Step struct {
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsid
	ID *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsif
	If *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsname
	Name *String
	Exec Exec
	// declaredUses retains a reference rejected by a conflicting execution key.
	DeclaredUses *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsenv
	Env *Env
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepscontinue-on-error
	ContinueOnError *Bool
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepstimeout-minutes
	TimeoutMinutes *Float
	// Background is 'background' field. When it is true, the Step runs asynchronously and the Workflow
	// immediately continues to the next Step.
	// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
	Background *Bool
	// Pos is a Position in source.
	Pos *Pos
}

// Credentials is Credentials configuration.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainercredentials
type Credentials struct {
	// Username is username for authentication.
	Username *String
	// Password is password for authentication.
	Password *String
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

// Container is configuration of how to run the Container.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainer
type Container struct {
	// Expression supplies an image String or complete Container mapping.
	Expression *String
	// Image is specification of Docker image.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerimage
	Image *String
	// Credentials is Credentials configuration of the Docker Container.
	Credentials *Credentials
	// Env is Environment variables setup in the Container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerenv
	Env *Env
	// Ports is list of port number mappings of the Container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerports
	Ports []*String
	// PortsExpression supplies the complete ports sequence.
	PortsExpression *String
	// Volumes are list of volumes to be mounted to the Container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainervolumes
	Volumes []*String
	// VolumesExpression supplies the complete volumes sequence.
	VolumesExpression *String
	// Options is options String to run the Container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontaineroptions
	Options *String
	// Command overrides Docker image's default command for Service containers.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservicesservice_idcommand
	Command *String
	// Entrypoint overrides Docker image's default entrypoint for Service containers.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservicesservice_identrypoint
	Entrypoint *String
	// Pos is a Position in source.
	Pos *Pos
}

// Service is configuration to run a Service like database.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservices
type Service struct {
	// Name is name of the Service.
	Name *String
	// Container is configuration of Container which runs the Service.
	Container *Container
}

// Services is a mapping from Service ID to its configuration.
// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#jobsjob_idservices
type Services struct {
	// Value is a mapping from Service ID to its Service instances. Keys are in lower case since
	// they are case-insensitive.
	Value map[string]*Service
	// Expression is an expression assigned to the Services mapping by ${{ }} placeholder. Otherwise
	// this field is nil.
	Expression *String
	// Pos is a Position in source.
	Pos *Pos
}

// Output is Output entry of the Job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idoutputs
type Output struct {
	// Name is name of Output.
	Name *String
	// Value is value of Output.
	Value *String
}

// Runner is struct for Runner configuration.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idruns-on
type Runner struct {
	// Expression supplies labels or a complete Runner selection mapping.
	Expression *String
	// Labels is list label names to select a Runner to run a Job. There are preset labels and user
	// defined labels. Runner matching to the labels is selected.
	Labels []*String
	// LabelsExpr is a String when expression syntax ${{ }} is used for this section. Related issue is #164.
	LabelsExpr *String
	// Group is a group of runners specified in runs-on: section. It is nil when no group is specified.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#choosing-runners-in-a-group
	Group *String
}

// WorkflowCallInput is a normal Input for Workflow call.
type WorkflowCallInput struct {
	// Name is a name of the Input.
	Name *String
	// Value is a value of the Input.
	Value *String
}

// WorkflowCallSecret is a secret Input for Workflow call.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idwith
type WorkflowCallSecret struct {
	// Name is a name of the secret
	Name *String
	// Value is a value of the secret
	Value *String
}

// WorkflowCall is a struct to represent Workflow call at jobs.<job_id>.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_iduses
type WorkflowCall struct {
	// Uses is a Workflow specification to be called. This field is mandatory.
	Uses *String
	// Inputs is a map from Input name to Input value at 'with:'. Keys are in lower case since Input names
	// are case-insensitive.
	Inputs map[string]*WorkflowCallInput
	// Secrets is a map from secret name to secret value at 'secrets:'. Keys are in lower case since Input
	// names are case-insensitive.
	Secrets map[string]*WorkflowCallSecret
	// InheritSecrets is true when 'secrets: inherit' is specified. In this case, Secrets must be empty.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#onworkflow_callsecretsinherit
	InheritSecrets bool
}

// Snapshot is a struct to represent image Snapshot at jobs.<job_id>.Snapshot.
// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#jobsjob_idsnapshot
// https://docs.github.com/en/actions/how-tos/manage-runners/larger-runners/use-custom-images
type Snapshot struct {
	// Expression supplies an image name or complete Snapshot mapping.
	Expression *String
	// ImageName is a name of the custom image.
	ImageName *String
	// Version is a version of the custom image.
	Version *String
	// If is a condition whether the custom image is used.
	If *String
}

// Job is configuration of how to run a Job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobs
type Job struct {
	// ID is an ID of the Job, which is key of Job configuration objects.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_id
	ID *String
	// Name is a name of Job that user can specify freely.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idname
	Name *String
	// Needs is list of Job IDs which should be run before running this Job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idneeds
	Needs []*String
	// RunsOn is Runner configuration which run the Job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idruns-on
	RunsOn *Runner
	// Permissions is permission configuration for running the Job.
	Permissions *Permissions
	// CacheMode overrides the Workflow's cache access for this Job. Nil means inherit.
	CacheMode *CacheMode
	// Environment is Environment specification where the Job runs.
	Environment *Environment
	// Concurrency is Concurrency configuration on running the Job.
	Concurrency *Concurrency
	// Outputs is map from Output name to Output specifications.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idoutputs
	Outputs map[string]*Output
	// Env is Environment variables setup while running the Job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idenv
	Env *Env
	// Defaults is default configurations of how to run scripts.
	Defaults *Defaults
	// If is a condition whether this Job should be run.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idif
	If *String
	// Steps is list of steps to be run in the Job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idsteps
	Steps []*Step
	// TimeoutMinutes is timeout value of running the Job in minutes.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
	TimeoutMinutes *Float
	// CancelTimeoutMinutes is the time allowed for cancellation cleanup in minutes.
	CancelTimeoutMinutes *Float
	// Strategy is Strategy configuration of running the Job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategy
	Strategy *Strategy
	// ContinueOnError is a flag to show if execution should continue on error.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontinue-on-error
	ContinueOnError *Bool
	// Container is Container configuration to run the Job.
	Container *Container
	// Services is a Services configurations.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservices
	Services *Services
	// WorkflowCall is a Workflow call by 'uses:'.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_iduses
	WorkflowCall *WorkflowCall
	// declaredUses retains a call rejected because it also declares Step-only keys.
	DeclaredUses *String
	// Snapshot is a custom image Snapshot.
	// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#jobsjob_idsnapshot
	Snapshot *Snapshot
	// Pos is a Position in source.
	Pos *Pos
}

// Workflow is root of Workflow syntax tree, which represents one Workflow configuration file.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions
type Workflow struct {
	ExpressionSources map[Pos]*ScriptSource
	// Name is name of the Workflow. This field can be nil when user didn't specify the name explicitly.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#name
	Name *String
	// Description describes the Workflow or reusable Workflow.
	Description *String
	// RunName is the name of Workflow runs. This field can be set dynamically using ${{ }}.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#run-name
	RunName *String
	// On is list of events which can trigger this Workflow.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onpushpull_requestbranchestags
	On []Event
	// Permissions is configuration of Permissions of this Workflow.
	Permissions *Permissions
	// CacheMode sets default cache access. Nil leaves the trigger-dependent default in effect.
	CacheMode *CacheMode
	// Env is a default set of Environment variables while running this Workflow.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Env
	Env *Env
	// Defaults is default configuration of how to run scripts.
	Defaults *Defaults
	// Concurrency is Concurrency configuration of entire Workflow. Each jobs also can their own
	// Concurrency configurations.
	Concurrency *Concurrency
	// Jobs is mappings from Job ID to the Job object. Keys are in lower case since they are case-insensitive.
	Jobs map[string]*Job
	// Pos is the first Workflow mapping key's Position. It may be nil for
	// workflows constructed directly by API callers.
	Pos *Pos
}

// FindWorkflowCallEvent returns workflow_call Event node if exists
func (w *Workflow) FindWorkflowCallEvent() (*WorkflowCallEvent, bool) {
	for _, e := range w.On {
		if e, ok := e.(*WorkflowCallEvent); ok {
			return e, true
		}
	}
	return nil, false
}

// ScriptLineSpan maps the text columns [Start, End) of one decoded script line to the YAML source.
// YamlEndLine and YamlEndColumn are nonzero for a decoded escape sequence.
type ScriptLineSpan struct {
	Line          int
	Start         int
	End           int
	YamlLine      int
	YamlColumn    int
	YamlEndLine   int
	YamlEndColumn int
}

// LineSpans projects the mapped spans onto text lines. A trailing newline or folded whitespace
// without a source maps its exclusive end to the end of the final mapped text.
func (s *ScriptSource) LineSpans() []ScriptLineSpan {
	if s == nil {
		return nil
	}
	var result []ScriptLineSpan
	var last *ScriptSourceSpan
	for i, line := range s.Lines {
		limit := line.End
		if limit < len(s.Value) {
			limit++
		}
		for j := range s.Spans {
			span := &s.Spans[j]
			start, end := max(span.Start, line.Start), min(span.End, limit)
			if span.Start >= span.End || start >= end {
				continue
			}
			last = span
			item := ScriptLineSpan{
				Line:       i + 1,
				Start:      utf8.RuneCountInString(s.Value[line.Start:start]) + 1,
				End:        utf8.RuneCountInString(s.Value[line.Start:end]) + 1,
				YamlLine:   span.YamlLine,
				YamlColumn: span.YamlColumn + utf8.RuneCountInString(s.Value[span.Start:start]),
			}
			if span.YamlEndColumn != 0 {
				item.YamlColumn, item.YamlEndLine, item.YamlEndColumn = span.YamlColumn, span.YamlEndLine, span.YamlEndColumn
				if item.YamlEndLine == 0 {
					item.YamlEndLine = span.YamlLine
				}
			}
			result = append(result, item)
			if end == limit && limit > line.End && i+1 < len(s.Lines) {
				separator := ScriptLineSpan{Line: i + 2, Start: 1, End: 1, YamlLine: span.YamlLine, YamlColumn: span.YamlColumn + utf8.RuneCountInString(s.Value[span.Start:end])}
				if span.YamlEndColumn != 0 {
					separator.YamlLine, separator.YamlColumn = item.YamlEndLine, span.YamlEndColumn
				}
				result = append(result, separator)
			}
		}
	}
	if last != nil && last.End < len(s.Value) && strings.Trim(s.Value[last.End:], " \n") == "" {
		tail := s.Lines[len(s.Lines)-1]
		column := last.YamlColumn + utf8.RuneCountInString(s.Value[last.Start:last.End])
		if last.YamlEndColumn != 0 {
			column = last.YamlEndColumn
		}
		line := last.YamlLine
		if last.YamlEndLine != 0 {
			line = last.YamlEndLine
		}
		at := utf8.RuneCountInString(s.Value[tail.Start:]) + 1
		result = append(result, ScriptLineSpan{Line: len(s.Lines), Start: at, End: at, YamlLine: line, YamlColumn: column})
	}
	return result
}
