package workflow

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"actionlint.kjanat.dev/v2/internal/input/source/text"
	exprsyntax "actionlint.kjanat.dev/v2/internal/languages/expressions/syntax"
	"actionlint.kjanat.dev/v2/internal/languages/yaml/schema"
)

// Pos represents a position in the file.
type Pos struct {
	// Line is the one-based line number.
	Line int
	// Col is the one-based column number.
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
	Comments     []ScalarComment
}

// ScalarComment is a YAML comment attached to a scalar declaration, never text
// inside its decoded value. Pos points to '#', in Unicode character columns.
type ScalarComment struct {
	Text     string
	Pos      Pos
	Trailing bool
}

// NewScriptSource creates source metadata for an already decoded scalar.
func NewScriptSource(value string) *ScriptSource {
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
	rel, ok := text.ByteOffsetAtColumn(s.Value[l.Start:l.End], col)
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
	i := s.spanIndex(offset, endPosition)
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

// spanIndex returns the first span ending after offset, or at offset for an end
// position. Spans are sorted by End; len(s.Spans) means no span qualifies.
func (s *ScriptSource) spanIndex(offset int, endPosition bool) int {
	low, high := 0, len(s.Spans)
	for low < high {
		mid := int(uint(low+high) >> 1)
		end := s.Spans[mid].End
		if end > offset || (endPosition && end == offset) {
			high = mid
		} else {
			low = mid + 1
		}
	}
	return low
}

func (p *Pos) String() string {
	return fmt.Sprintf("line:%d,col:%d", p.Line, p.Col)
}

// IsBefore reports whether p precedes other. Equal positions return false.
func (p *Pos) IsBefore(other *Pos) bool {
	if p.Line < other.Line {
		return true
	}
	if p.Line > other.Line {
		return false
	}
	return p.Col < other.Col
}

// SourceScalarKind identifies the YAML node kind captured from source.
type SourceScalarKind uint8

const (
	SourceScalarUnknown SourceScalarKind = iota
	SourceScalarDocument
	SourceScalarSequence
	SourceScalarMapping
	SourceScalarScalar
	SourceScalarAlias
)

// SourceScalar is an immutable, pointer-free snapshot of a YAML node at parse time.
type SourceScalar struct {
	HasSource bool
	Evaluated bool
	Kind      SourceScalarKind
	Tag       string
	Value     string
	Pos       Pos
	// End is the exclusive physical end of a dependency scalar, when available.
	End Pos
}

// String represents a YAML string value and its source position.
type String struct {
	// Value is the decoded string.
	Value string
	// Quoted reports whether the YAML source uses single or double quotes.
	Quoted bool
	// Pos is the string's position in the source.
	Pos *Pos
	// SourceScalar records the original YAML node before normalization or folding.
	SourceScalar SourceScalar
}

// ContainsExpression returns whether the String contains at least one ${{ }} expression.
func (s *String) ContainsExpression() bool {
	return exprsyntax.ContainsExpression(s.Value)
}

func IsExprAssigned(s string) bool {
	if !strings.HasPrefix(s, "${{") || !strings.HasSuffix(s, "}}") {
		return false
	}
	// Delimiters inside single-quoted strings are data. Doubled quotes toggle
	// twice, keeping the scan inside the same string literal.
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
	// Let the expression parser diagnose an unterminated string literal.
	return inString
}

// IsExpressionAssigned returns whether a single expression is assigned to the String.
func (s *String) IsExpressionAssigned() bool {
	return IsExprAssigned(s.Value)
}

// Bool represents a YAML boolean or expression and its source position.
type Bool struct {
	// Value is the decoded boolean.
	Value bool
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
	// Pos is the source position.
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

// Int represents a YAML integer or expression and its source position.
type Int struct {
	// Value is the decoded integer.
	Value int
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
	// TruncatedFrom is the source spelling of a fractional literal GitHub
	// truncates toward zero to produce Value. It is empty for whole numbers.
	TruncatedFrom string
	// Pos is the source position.
	Pos *Pos
}

// Float represents a YAML number or expression and its source position.
type Float struct {
	// Value is the decoded number.
	Value float64
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
	// Pos is the source position.
	Pos *Pos
}

// Event represents an event in the workflow on section.
type Event interface {
	// EventName returns the event name that triggers the workflow.
	EventName() string
}

// WebhookEventFilter describes a webhook filter, such as branches or paths-ignore.
// Some filters are mutually exclusive.
// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#using-filters
type WebhookEventFilter struct {
	// Name is the filter name, such as branches or tags.
	Name *String
	// Values is a list of filter values.
	Values []*String
}

// IsEmpty reports whether the filter is nil or has no values.
func (f *WebhookEventFilter) IsEmpty() bool {
	return f == nil || len(f.Values) == 0
}

// WebhookEvent configures a webhook trigger.
// Some events cannot specify types. The push event accepts tag and path filters.
// The pull_request and pull_request_target events accept path filters.
// Only workflow_run accepts workflows.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onevent_nametypes
type WebhookEvent struct {
	// Hook is the webhook event name.
	Hook *String
	// Types lists the webhook activity types that trigger the workflow.
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
	// Workflows lists names selected by the workflow_run event.
	Workflows []*String
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *WebhookEvent) EventName() string {
	return e.Hook.Value
}

// ScheduleEntry is one entry in a schedule event.
// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#onschedule
type ScheduleEntry struct {
	// Cron is the cron expression for the schedule.
	Cron *String
	// Timezone is the optional IANA timezone for the schedule.
	Timezone *String
}

// ScheduledEvent configures a workflow schedule.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#scheduled-events
type ScheduledEvent struct {
	// Schedules lists the workflow schedule entries.
	Schedules []*ScheduleEntry
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *ScheduledEvent) EventName() string {
	return "schedule"
}

// WorkflowDispatchEventInputType identifies a workflow_dispatch input type.
// https://github.blog/changelog/2021-11-10-github-actions-Input-types-for-manual-workflows/
type WorkflowDispatchEventInputType uint8

const (
	// WorkflowDispatchEventInputTypeNone means no input type was specified.
	WorkflowDispatchEventInputTypeNone WorkflowDispatchEventInputType = iota
	// WorkflowDispatchEventInputTypeString identifies a string input.
	WorkflowDispatchEventInputTypeString
	// WorkflowDispatchEventInputTypeNumber identifies a number input.
	WorkflowDispatchEventInputTypeNumber
	// WorkflowDispatchEventInputTypeBoolean identifies a boolean input.
	WorkflowDispatchEventInputTypeBoolean
	// WorkflowDispatchEventInputTypeChoice identifies a choice input.
	WorkflowDispatchEventInputTypeChoice
	// WorkflowDispatchEventInputTypeEnvironment identifies an environment input.
	WorkflowDispatchEventInputTypeEnvironment
)

// DispatchInput describes an input for a manually dispatched workflow.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type DispatchInput struct {
	// Name is the input name.
	Name *String
	// Description describes the input.
	Description *String
	// Required indicates whether the input is mandatory.
	Required *Bool
	// Default is the input default.
	Default *String
	// Type is the input type.
	// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
	Type WorkflowDispatchEventInputType
	// Options lists the values for a choice input.
	Options []*String
}

// WorkflowDispatchEvent configures manual workflow dispatch.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type WorkflowDispatchEvent struct {
	// Inputs maps lowercase input names to their attributes. Names are case insensitive.
	Inputs map[string]*DispatchInput
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *WorkflowDispatchEvent) EventName() string {
	return "workflow_dispatch"
}

// RepositoryDispatchEvent configures the repository_dispatch event.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#repository_dispatch
type RepositoryDispatchEvent struct {
	// Types lists the activity types that can trigger the workflow.
	Types []*String
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *RepositoryDispatchEvent) EventName() string {
	return "repository_dispatch"
}

// WorkflowCallEventInputType identifies a workflow_call input type.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinput_idtype
type WorkflowCallEventInputType uint8

const (
	// WorkflowCallEventInputTypeInvalid is the zero value and represents an invalid type.
	WorkflowCallEventInputTypeInvalid WorkflowCallEventInputType = iota
	// WorkflowCallEventInputTypeBoolean identifies a boolean input.
	WorkflowCallEventInputTypeBoolean
	// WorkflowCallEventInputTypeNumber identifies a number input.
	WorkflowCallEventInputTypeNumber
	// WorkflowCallEventInputTypeString identifies a string input.
	WorkflowCallEventInputTypeString
)

// WorkflowCallEventInput describes a workflow_call input.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinputs
type WorkflowCallEventInput struct {
	// Name is the input name.
	Name *String
	// Description describes the input.
	Description *String
	// Default is the input default. Nil means no default was specified.
	Default *String
	// Required indicates whether the input is required. Nil means it was not
	// specified and defaults to optional.
	Required *Bool
	// Type is required and must be boolean, number or string.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinput_idtype
	Type WorkflowCallEventInputType
	// ID is the lowercase input ID. Input IDs are case insensitive.
	ID string
}

// IsRequired reports whether the input is marked as required.
func (i *WorkflowCallEventInput) IsRequired() bool {
	return i.Required != nil && i.Required.Value
}

// WorkflowCallEventSecret describes a workflow_call secret.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecrets
type WorkflowCallEventSecret struct {
	// Name is the secret name.
	Name *String
	// Description describes the secret.
	Description *String
	// Required indicates whether the secret is required. Nil means optional.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecretssecret_idrequired
	Required *Bool
}

// WorkflowCallEventOutput describes a workflow_call output.
// https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-Workflow
type WorkflowCallEventOutput struct {
	// Name is the output name.
	Name *String
	// Description describes the output.
	Description *String
	// Value is an expression for the output value.
	Value *String
}

// WorkflowCallEvent configures the workflow_call event.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#Workflow-reuse-events
type WorkflowCallEvent struct {
	// Inputs lists workflow_call inputs in source order, which matters when
	// checking their default values.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callinputs
	Inputs []*WorkflowCallEventInput
	// Secrets maps lowercase names to secret declarations. Nil means 'secrets'
	// was omitted. Names are case insensitive.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onworkflow_callsecrets
	Secrets map[string]*WorkflowCallEventSecret
	// Outputs maps lowercase names to output declarations. Names are case insensitive.
	// https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-Workflow
	Outputs map[string]*WorkflowCallEventOutput
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *WorkflowCallEvent) EventName() string {
	return "workflow_call"
}

// ImageVersionEvent configures the image_version event.
// https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#image_version
type ImageVersionEvent struct {
	// Types filters image version activity to created, ready, or deleted events.
	Types []*String
	// Names lists names matched against created image names.
	Names []*String
	// Versions lists patterns matching created image versions. Glob patterns are allowed.
	Versions []*String
	// Pos is the source position.
	Pos *Pos
}

// EventName returns the event name that triggers the workflow.
func (e *ImageVersionEvent) EventName() string {
	return "image_version"
}

// PermissionScope configures a token permission scope, such as "issues" or "checks".
// https://docs.github.com/en/actions/security-for-github-actions/security-guides/automatic-token-authentication#Permissions-for-the-github_token
type PermissionScope struct {
	// Name is the scope name.
	Name *String
	// Value is the access level for the scope.
	Value *String
}

// Permissions configures all token scopes together or individual scopes.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Permissions
type Permissions struct {
	// All represents a permission value for all the scopes at once.
	All *String
	// Scopes maps scope names to their permission configurations.
	Scopes map[string]*PermissionScope
	// Pos is the source position.
	Pos *Pos
}

// DefaultsRun configures the shell and working directory for run steps.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#defaultsrun
type DefaultsRun struct {
	// Expression supplies the complete run Defaults mapping.
	Expression *String
	// Shell names the shell to run.
	Shell *String
	// WorkingDirectory is a default working directory path.
	WorkingDirectory *String
	// Pos is the source position.
	Pos *Pos
}

// Defaults contains default settings for run steps.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Defaults
type Defaults struct {
	// Run configures defaults for run steps.
	Run *DefaultsRun
	// Pos is the source position.
	Pos *Pos
}

// Concurrency configures a workflow or job concurrency group.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Concurrency
type Concurrency struct {
	// Expression supplies a group string or complete concurrency mapping.
	Expression *String
	// Group is the concurrency group name.
	Group *String
	// CancelInProgress controls cancellation of other jobs or workflow runs in the group.
	CancelInProgress *Bool
	// Queue sets the strategy for pending jobs or workflow runs: "single" (default) or "max".
	Queue *String
	// Pos is the source position.
	Pos *Pos
}

// Environment configures a deployment environment.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idenvironment
type Environment struct {
	// Expression supplies an environment name or complete environment mapping.
	Expression *String
	// Name is the environment name.
	Name *String
	// URL is the URL mapped to 'environment_url' in the deployments API. Empty value means no value was specified.
	URL *String
	// Deployment controls whether GitHub creates a deployment. False skips
	// automatic deployment but retains environment protection rules and secrets.
	Deployment *Bool
	// Pos is the source position.
	Pos *Pos
}

// ExecKind identifies how a step is executed.
type ExecKind uint8

const (
	// ExecKindAction identifies a step that runs an action.
	ExecKindAction ExecKind = iota
	// ExecKindRun identifies a step that runs a shell script.
	ExecKindRun
	// ExecKindWait identifies a wait or wait-all step.
	ExecKindWait
	// ExecKindCancel identifies a step that cancels a background step.
	ExecKindCancel
	// ExecKindParallel identifies a group of steps that run in parallel.
	ExecKindParallel
)

// Exec identifies how a workflow step is executed.
type Exec interface {
	// Kind returns the step execution kind.
	Kind() ExecKind
}

// ExecRun configures a step that runs a shell script.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsrun
type ExecRun struct {
	// Run is the script to execute.
	Run *String
	// Source maps positions in Run.Value back to the original YAML source when
	// they can be mapped exactly.
	Source *ScriptSource
	// Shell is the optional shell field. Nil means it was not specified.
	Shell *String
	// WorkingDirectory is the optional working-directory field. Nil means it was not specified.
	WorkingDirectory *String
	// RunPos is the position of the run field.
	RunPos *Pos
}

// Kind returns the step execution kind.
func (e *ExecRun) Kind() ExecKind {
	return ExecKindRun
}

// Input describes an input passed to an action.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswith
type Input struct {
	// Name is the input name.
	Name *String
	// Value is the input value.
	Value *String
	// Source maps the decoded input back to its declaring YAML scalar.
	Source *ScriptSource
}

// ActionInputMapping is a literal or expression-valued action input mapping.
type ActionInputMapping struct {
	// Inputs contains named input values.
	Inputs     map[string]*Input
	Expression *String
	// Insertions contains nested mapping directives.
	Insertions []*ActionInputMapping
}

// ExecAction configures a step that runs an action.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsuses
type ExecAction struct {
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsuses
	// Uses is nil when its value could not be parsed as a nonempty string.
	Uses *String
	// Inputs maps lowercase names to action inputs from 'with'. Names are case insensitive.
	Inputs map[string]*Input
	// InputsExpression supplies the complete action inputs mapping.
	InputsExpression *String
	// InputInsertions supplies additional action inputs through mapping directives.
	InputInsertions []*ActionInputMapping
	// Entrypoint is the optional 'entrypoint' input. Nil means it was not specified.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswithentrypoint
	Entrypoint *String
	// Args is the optional 'args' input. Nil means it was not specified.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepswithargs
	Args *String

	// Constant mapping results must not be interpreted as expression source.
	EntrypointEvaluated  bool
	ArgsEvaluated        bool
	InputNameExpressions []*String
}

// Kind returns the step execution kind.
func (e *ExecAction) Kind() ExecKind {
	return ExecKindAction
}

// ExecWait configures a wait or wait-all step for background steps.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecWait struct {
	// Names lists background step IDs from the 'wait' field. It is nil when
	// 'wait-all' is used instead.
	Names []*String
	// All is true when the step waits for all preceding background steps.
	All bool
	// AllPos is the position of 'wait-all'. It is nil when 'wait' is used instead.
	AllPos *Pos
}

// Kind returns the step execution kind.
func (e *ExecWait) Kind() ExecKind {
	return ExecKindWait
}

// ExecCancel configures a step that cancels a running background step.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecCancel struct {
	// Name is the ID of the background step to cancel.
	Name *String
}

// Kind returns the step execution kind.
func (e *ExecCancel) Kind() ExecKind {
	return ExecKindCancel
}

// ExecParallel configures a group of steps that run in parallel.
// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
type ExecParallel struct {
	// Steps is the group of steps to run in parallel, given by the 'parallel' field.
	Steps []*Step
}

// Kind returns the step execution kind.
func (e *ExecParallel) Kind() ExecKind {
	return ExecKindParallel
}

// RawYAMLValueKind identifies the kind of a raw YAML value.
type RawYAMLValueKind int

const (
	// RawYAMLValueKindObject identifies a mapping.
	RawYAMLValueKindObject = RawYAMLValueKind(4)
	// RawYAMLValueKindArray identifies a sequence.
	RawYAMLValueKindArray = RawYAMLValueKind(2)
	// RawYAMLValueKindString identifies a scalar.
	RawYAMLValueKindString = RawYAMLValueKind(8)
)

// RawYAMLValue is a matrix value, which can include mappings and arrays.
type RawYAMLValue interface {
	// Kind returns the kind of the raw YAML value.
	Kind() RawYAMLValueKind
	// Equals reports whether the other value is equal.
	Equals(other RawYAMLValue) bool
	// Pos returns the value's start position in the source file.
	Pos() *Pos
	// String returns the string representation of the value.
	String() string
}

// RawYAMLObject is a raw YAML mapping value.
type RawYAMLObject struct {
	// Props maps lowercase property names to values. Names are case insensitive.
	Props    map[string]RawYAMLValue
	Position *Pos
}

// Kind returns the kind of the raw YAML value.
func (o *RawYAMLObject) Kind() RawYAMLValueKind {
	return RawYAMLValueKindObject
}

// Equals reports whether the other value is equal.
func (o *RawYAMLObject) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLObject:
		if len(o.Props) != len(other.Props) {
			return false
		}
		for n, p1 := range o.Props {
			if p2, ok := other.Props[n]; !ok || !rawEquals(p1, p2) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Pos returns a copy of the value's start position in the source file.
func (o *RawYAMLObject) Pos() *Pos {
	return copyPos(o.Position)
}

func (o *RawYAMLObject) String() string {
	qs := make([]string, 0, len(o.Props))
	for n, p := range o.Props {
		qs = append(qs, fmt.Sprintf("%q: %s", n, rawString(p)))
	}
	sort.Strings(qs)
	return "{" + strings.Join(qs, ", ") + "}"
}

// RawYAMLArray is a raw YAML sequence value.
type RawYAMLArray struct {
	// Elems lists the array elements.
	Elems    []RawYAMLValue
	Position *Pos
}

// Kind returns the kind of the raw YAML value.
func (a *RawYAMLArray) Kind() RawYAMLValueKind {
	return RawYAMLValueKindArray
}

// Equals reports whether the other value is equal.
func (a *RawYAMLArray) Equals(other RawYAMLValue) bool {
	switch other := other.(type) {
	case *RawYAMLArray:
		if len(a.Elems) != len(other.Elems) {
			return false
		}
		for i, e1 := range a.Elems {
			if !rawEquals(e1, other.Elems[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Pos returns a copy of the value's start position in the source file.
func (a *RawYAMLArray) Pos() *Pos {
	return copyPos(a.Position)
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
		b.WriteString(rawString(e))
	}

	b.WriteRune(']')
	return b.String()
}

// RawYAMLString is a raw YAML scalar value.
type RawYAMLString struct {
	// Value is the scalar node's string representation.
	Value string
	// Tag is the YAML tag of the scalar node, such as "!!str" or "!!Int".
	// A tag rejected by the parser is recorded as "!!str".
	Tag      string
	Position *Pos
}

// Kind returns the kind of the raw YAML value.
func (s *RawYAMLString) Kind() RawYAMLValueKind {
	return RawYAMLValueKindString
}

// Equals reports whether the other value is equal.
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
		if schema.IsCoreSchemaBool(s.Value) {
			return strings.EqualFold(s.Value, "true")
		}
	case YamlTagNull:
		if schema.IsCoreSchemaNull(s.Value) {
			return nil
		}
	case YamlTagInt, YamlTagFloat:
		if !schema.IsCoreSchemaNumber(s.Value) {
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

// Pos returns a copy of the value's start position in the source file.
func (s *RawYAMLString) Pos() *Pos {
	return copyPos(s.Position)
}

// copyPos detaches a node position so callers cannot move the node through it.
func copyPos(p *Pos) *Pos {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// rawEquals dispatches over the closed set of raw YAML node types so nested
// comparisons stay within statically known methods.
func rawEquals(a, b RawYAMLValue) bool {
	switch a := a.(type) {
	case *RawYAMLObject:
		return a.Equals(b)
	case *RawYAMLArray:
		return a.Equals(b)
	case *RawYAMLString:
		return a.Equals(b)
	default:
		return false
	}
}

// rawString renders a nested raw YAML node through its concrete method.
func rawString(v RawYAMLValue) string {
	switch v := v.(type) {
	case *RawYAMLObject:
		return v.String()
	case *RawYAMLArray:
		return v.String()
	case *RawYAMLString:
		return v.String()
	default:
		return ""
	}
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

// MatrixRow declares one matrix axis and the values it can take.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixRow struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	// Name is the matrix axis name.
	Name *String
	// Values lists the values the axis can take.
	Values []RawYAMLValue
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
}

// MatrixAssign assigns a value to a matrix axis.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixAssign struct {
	// Key is the matrix axis name.
	Key *String
	// Value is the value assigned to the axis.
	Value RawYAMLValue
}

// MatrixCombination is a set of matrix value assignments.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixCombination struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	Assigns   map[string]*MatrixAssign
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
}

// MatrixCombinations lists assignments in an include or exclude section.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixCombinations struct {
	// Name is the include/exclude key as written in source.
	Name         *String
	Evaluated    bool
	Combinations []*MatrixCombination
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
}

// ContainsExpression reports whether the combinations include an expression.
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

// Matrix configures the combinations for a job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type Matrix struct {
	// Insertions supplies additional axes or filters through mapping directives.
	Insertions []*Matrix
	// Rows maps lowercase axis names to their values. Names are case insensitive.
	Rows map[string]*MatrixRow
	// Include lists matrix combinations and values to add to combinations.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#example-including-additional-values-into-combinations
	Include *MatrixCombinations
	// Exclude lists matrix combinations that should not run.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#example-excluding-configurations-from-a-Matrix
	Exclude *MatrixCombinations
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
	// Pos is the source position.
	Pos *Pos
}

// Strategy configures how a job runs its matrix combinations.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategy
type Strategy struct {
	// Expression supplies the complete strategy mapping.
	Expression *String
	// Matrix defines combinations of values. Each combination runs the job once.
	Matrix *Matrix
	// FailFast indicates whether other jobs should stop when one job fails.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategyfail-fast
	FailFast *Bool
	// MaxParallel limits how many jobs run at once.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategymax-parallel
	MaxParallel *Int
	// Pos is the source position.
	Pos *Pos
}

// EnvVar declares an environment variable and its value.
type EnvVar struct {
	// Name is the environment variable name.
	Name *String
	// Value is the environment variable value.
	Value *String
}

// Env declares a set of environment variables.
type Env struct {
	// Vars maps environment variable names to their values.
	Vars map[string]*EnvVar
	// Insertions contains environment mappings or object expressions supplied by ${{ insert }} directives.
	Insertions []*Env
	// Expression contains a ${{ ... }} expression. When it is not empty,
	// Vars and Insertions should be nil.
	Expression *String
}

// Step configures a workflow step.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idsteps
type Step struct {
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsid
	ID *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsif
	If *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsname
	Name *String
	Exec Exec
	// DeclaredUses records uses even when a conflicting execution key invalidates it.
	DeclaredUses *String
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepsenv
	Env *Env
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepscontinue-on-error
	ContinueOnError *Bool
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstepstimeout-minutes
	TimeoutMinutes *Float
	// Background runs the step asynchronously when true, allowing the workflow
	// to continue immediately to the next step.
	// https://github.blog/changelog/2026-06-25-actions-steps-can-now-be-run-in-parallel/
	Background *Bool
	// Pos is the source position.
	Pos *Pos
}

// Credentials configures authentication for a container.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainercredentials
type Credentials struct {
	// Username is the username for authentication.
	Username *String
	// Password is the password for authentication.
	Password *String
	// Expression contains the expression when this field uses ${{ }} syntax.
	Expression *String
	// Pos is the source position.
	Pos *Pos
}

// Container configures a job or service container.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainer
type Container struct {
	// Expression supplies an image string or complete container mapping.
	Expression *String
	// Image identifies the Docker image.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerimage
	Image *String
	// Credentials configures authentication for the Docker image.
	Credentials *Credentials
	// Env configures environment variables in the container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerenv
	Env *Env
	// Ports lists container port mappings.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainerports
	Ports []*String
	// PortsExpression supplies the complete ports sequence.
	PortsExpression *String
	// Volumes lists the volumes to mount in the container.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontainervolumes
	Volumes []*String
	// VolumesExpression supplies the complete volumes sequence.
	VolumesExpression *String
	// Options supplies container options.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontaineroptions
	Options *String
	// Command overrides the Docker image's default command for service containers.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservicesservice_idcommand
	Command *String
	// Entrypoint overrides the Docker image's default entrypoint for service containers.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservicesservice_identrypoint
	Entrypoint *String
	// Pos is the source position.
	Pos *Pos
}

// Service configures a service such as a database.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservices
type Service struct {
	// Name is the service name.
	Name *String
	// Container configures the container that runs the service.
	Container *Container
}

// Services maps service IDs to their configurations.
// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#jobsjob_idservices
type Services struct {
	// Value maps lowercase service IDs to services. IDs are case insensitive.
	Value map[string]*Service
	// Expression is an expression assigned to the Services mapping by ${{ }} placeholder. Otherwise
	// this field is nil.
	Expression *String
	// Pos is the source position.
	Pos *Pos
}

// Output describes a job output.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idoutputs
type Output struct {
	// Name is the output name.
	Name *String
	// Value is the output value.
	Value *String
}

// Runner configures runner selection for a job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idruns-on
type Runner struct {
	// Expression supplies labels or a complete runner selection mapping.
	Expression *String
	// Labels selects a runner by preset or user-defined labels.
	Labels []*String
	// LabelsExpr contains a ${{ }} expression for the labels. See issue #164.
	LabelsExpr *String
	// Group is a group of runners specified in runs-on: section. It is nil when no group is specified.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#choosing-runners-in-a-group
	Group *String
}

// WorkflowCallInput describes an input passed to a reusable workflow.
type WorkflowCallInput struct {
	// Name is the input name.
	Name *String
	// Value is the input value.
	Value *String
}

// WorkflowCallSecret describes a secret passed to a reusable workflow.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idwith
type WorkflowCallSecret struct {
	// Name is the secret name.
	Name *String
	// Value is the secret value.
	Value *String
}

// WorkflowCall represents a reusable workflow call at jobs.<job_id>.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_iduses
type WorkflowCall struct {
	// Uses identifies the reusable workflow to call. This field is required.
	Uses *String
	// Inputs maps lowercase input names to values from 'with:'. Names are case insensitive.
	Inputs map[string]*WorkflowCallInput
	// Secrets maps lowercase secret names to values from 'secrets:'. Names are case insensitive.
	Secrets map[string]*WorkflowCallSecret
	// InheritSecrets is true when 'secrets: inherit' is specified. In this case, Secrets must be empty.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#onworkflow_callsecretsinherit
	InheritSecrets bool
}

// Snapshot configures a custom image snapshot for a job.
// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#jobsjob_idsnapshot
// https://docs.github.com/en/actions/how-tos/manage-runners/larger-runners/use-custom-images
type Snapshot struct {
	// Expression supplies an image name or complete Snapshot mapping.
	Expression *String
	// ImageName is the custom image name.
	ImageName *String
	// Version is the custom image version.
	Version *String
	// If controls whether the custom image is used.
	If *String
}

// Job configures a workflow job.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobs
type Job struct {
	// ID is the job ID, which is the key of its configuration.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_id
	ID *String
	// Name is the user-specified job name.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idname
	Name *String
	// Needs lists job IDs that must run before this job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idneeds
	Needs []*String
	// RunsOn configures the runner for the job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idruns-on
	RunsOn *Runner
	// Permissions configures the job token permissions.
	Permissions *Permissions
	// CacheMode overrides workflow cache access for this job. Nil means inherit.
	CacheMode *CacheMode
	// Environment selects the deployment environment for the job.
	Environment *Environment
	// Concurrency configures the job concurrency group.
	Concurrency *Concurrency
	// Outputs maps output names to their declarations.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idoutputs
	Outputs map[string]*Output
	// Env configures environment variables for the job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idenv
	Env *Env
	// Defaults configures default settings for scripts.
	Defaults *Defaults
	// If controls whether the job runs.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idif
	If *String
	// Steps lists the steps in the job.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idsteps
	Steps []*Step
	// TimeoutMinutes is the job timeout in minutes.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idtimeout-minutes
	TimeoutMinutes *Float
	// CancelTimeoutMinutes is the time allowed for cancellation cleanup in minutes.
	CancelTimeoutMinutes *Float
	// Strategy configures how the job runs.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idstrategy
	Strategy *Strategy
	// ContinueOnError controls whether execution continues after an error.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idcontinue-on-error
	ContinueOnError *Bool
	// Container configures the job container.
	Container *Container
	// Services configures the job service containers.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_idservices
	Services *Services
	// WorkflowCall describes a reusable workflow call.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#jobsjob_iduses
	WorkflowCall *WorkflowCall
	// DeclaredUses records uses even when step-only keys invalidate the call.
	DeclaredUses *String
	// Snapshot configures a custom image snapshot.
	// https://docs.github.com/en/actions/reference/workflows-and-actions/Workflow-syntax#jobsjob_idsnapshot
	Snapshot *Snapshot
	// Pos is the source position.
	Pos *Pos
}

// Workflow is the root of the syntax tree for one workflow file.
// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions
type Workflow struct {
	ExpressionSources map[Pos]*ScriptSource
	// Name is the workflow name. It is nil when no name was specified.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#name
	Name *String
	// Description describes the workflow.
	Description *String
	// RunName names workflow runs. It can use a ${{ }} expression.
	// https://docs.github.com/en/actions/using-workflows/Workflow-syntax-for-github-actions#run-name
	RunName *String
	// On lists the events that can trigger the workflow.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#onpushpull_requestbranchestags
	On []Event
	// OnPos is the position of the "on" key. It is nil when the key is absent.
	OnPos *Pos
	// Permissions configures workflow token permissions.
	Permissions *Permissions
	// CacheMode sets default cache access. Nil leaves the trigger-dependent default in effect.
	CacheMode *CacheMode
	// Env configures default environment variables for the workflow.
	// https://docs.github.com/en/actions/learn-github-actions/Workflow-syntax-for-github-actions#Env
	Env *Env
	// Defaults configures default settings for scripts.
	Defaults *Defaults
	// Concurrency configures the workflow concurrency group. Jobs can also
	// configure their own groups.
	Concurrency *Concurrency
	// Jobs maps lowercase job IDs to jobs. IDs are case insensitive.
	Jobs map[string]*Job
	// Pos is the position of the first workflow mapping key. It may be nil for
	// workflows constructed directly by API callers.
	Pos *Pos
}

// FindWorkflowCallEvent returns the workflow_call event when present.
func FindWorkflowCallEvent(w *Workflow) (*WorkflowCallEvent, bool) {
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
