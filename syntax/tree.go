package syntax

// The tree is a detached value copy of the parser's internal representation.
// Callers may read or modify it freely; parser metadata that maps decoded
// scalars back to YAML source is not part of this contract.

// Pos represents a position in the file.
type Pos struct {
	// Line is a line number of the position. This value is 1-based.
	Line int
	// Col is a column number of the position. This value is 1-based.
	Col int
}

// String represents a YAML string value and its source position.
type String struct {
	// Value is a raw value of the String.
	Value string
	// Quoted represents the String is quoted with ' or " in the YAML source.
	Quoted bool
	// Pos is a position of the String in source.
	Pos *Pos
}

// Bool represents generic boolean value in YAML file with position.
type Bool struct {
	// Value is a raw value of the Bool String.
	Value bool
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Int represents generic integer value in YAML file with position.
type Int struct {
	// Value is a raw value of the integer String.
	Value int
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Float represents generic Float value in YAML file with position.
type Float struct {
	// Value is a raw value of the Float String.
	Value float64
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Event interface represents workflow events in 'on' section.
type Event interface {
	// EventName returns name of the Event to trigger this workflow.
	EventName() string
}

// WebhookEventFilter is a filter for webhook events such as 'branches', 'paths-ignore', ...
// https://docs.github.com/en/actions/using-workflows/workflow-syntax-for-github-actions#using-filters
type WebhookEventFilter struct {
	// Name is a name of filter such like 'branches', 'tags'.
	Name *String
	// Values is a list of filter values.
	Values []*String
}

// WebhookEvent represents Event type based on webhook events.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onevent_nametypes
type WebhookEvent struct {
	// Hook is a name of the webhook Event.
	Hook *String
	// Types is list of types of the webhook Event.
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
	// Workflows is list of workflow names which are triggered by 'workflow_run' Event.
	Workflows []*String
	// Pos is a position in source.
	Pos *Pos
}

// ScheduleEntry is a single entry in a schedule Event.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onschedule
type ScheduleEntry struct {
	// Cron is the cron String for the schedule.
	Cron *String
	// Timezone is the optional IANA timezone for the schedule.
	Timezone *String
}

// ScheduledEvent is Event scheduled by workflow.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#scheduled-events
type ScheduledEvent struct {
	// Schedules is list of schedule entries which schedule the workflow.
	Schedules []*ScheduleEntry
	// Pos is a position in source.
	Pos *Pos
}

// WorkflowDispatchEventInputType is a type for input types of workflow_dispatch events.
type WorkflowDispatchEventInputType uint8

const (
	// WorkflowDispatchEventInputTypeNone represents no type is specified to the input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeNone WorkflowDispatchEventInputType = iota
	// WorkflowDispatchEventInputTypeString is String type of input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeString
	// WorkflowDispatchEventInputTypeNumber is number type of input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeNumber
	// WorkflowDispatchEventInputTypeBoolean is boolean type of input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeBoolean
	// WorkflowDispatchEventInputTypeChoice is choice type of input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeChoice
	// WorkflowDispatchEventInputTypeEnvironment is Environment type of input of workflow_dispatch Event.
	WorkflowDispatchEventInputTypeEnvironment
)

// DispatchInput is input specified on dispatching workflow manually.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type DispatchInput struct {
	// Name is a name of input value specified on dispatching workflow manually.
	Name *String
	// Description is a description of input value specified on dispatching workflow manually.
	Description *String
	// Required is a flag to show if this input is mandatory or not on dispatching workflow manually.
	Required *Bool
	// Default is a default value of input value on dispatching workflow manually.
	Default *String
	// Type is a type of the input.
	Type WorkflowDispatchEventInputType
	// Options is list of options of choice type.
	Options []*String
}

// WorkflowDispatchEvent is Event on dispatching workflow manually.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow_dispatch
type WorkflowDispatchEvent struct {
	// Inputs is map from input names to input attributes. Keys are in lower case since they are case insensitive.
	Inputs map[string]*DispatchInput
	// Pos is a position in source.
	Pos *Pos
}

// RepositoryDispatchEvent is repository_dispatch Event configuration.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#repository_dispatch
type RepositoryDispatchEvent struct {
	// Types is list of types which can trigger workflow.
	Types []*String
	// Pos is a position in source.
	Pos *Pos
}

// WorkflowCallEventInputType is a type of inputs at workflow_call Event.
type WorkflowCallEventInputType uint8

const (
	// WorkflowCallEventInputTypeInvalid represents invalid type input as default value of the type.
	WorkflowCallEventInputTypeInvalid WorkflowCallEventInputType = iota
	// WorkflowCallEventInputTypeBoolean represents boolean type input.
	WorkflowCallEventInputTypeBoolean
	// WorkflowCallEventInputTypeNumber represents number type input.
	WorkflowCallEventInputTypeNumber
	// WorkflowCallEventInputTypeString represents String type input.
	WorkflowCallEventInputTypeString
)

// WorkflowCallEventInput is an input configuration of workflow_call Event.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onworkflow_callinputs
type WorkflowCallEventInput struct {
	// Name is a name of the input.
	Name *String
	// Description is a description of the input.
	Description *String
	// Default is a default value of the input. Nil means no default value.
	Default *String
	// Required represents if the input is required or optional. Nil means it was not explicitly specified.
	Required *Bool
	// Type of the input, which must be one of 'boolean', 'number' or 'string'.
	Type WorkflowCallEventInputType
	// ID is an ID of the input. Input ID is in lower case because it is case-insensitive.
	ID string
}

// WorkflowCallEventSecret is a secret configuration of workflow_call Event.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#onworkflow_callsecrets
type WorkflowCallEventSecret struct {
	// Name is a name of the secret.
	Name *String
	// Description is a description of the secret.
	Description *String
	// Required represents if the secret is required or optional. Nil means optional.
	Required *Bool
}

// WorkflowCallEventOutput is an output configuration of workflow_call Event.
// https://docs.github.com/en/actions/using-workflows/reusing-workflows#using-outputs-from-a-reusable-workflow
type WorkflowCallEventOutput struct {
	// Name is a name of the output.
	Name *String
	// Description is a description of the output.
	Description *String
	// Value is an expression for the value of the output.
	Value *String
}

// WorkflowCallEvent is workflow_call Event configuration.
// https://docs.github.com/en/actions/learn-github-actions/events-that-trigger-workflows#workflow-reuse-events
type WorkflowCallEvent struct {
	// Inputs is an array of inputs of the workflow_call Event, in declaration order.
	Inputs []*WorkflowCallEventInput
	// Secrets is a map from name of secret to secret configuration. Nil when 'secrets' is omitted.
	// Keys are in lower case since they are case-insensitive.
	Secrets map[string]*WorkflowCallEventSecret
	// Outputs is a map from name of output to output configuration. Keys are in lower case.
	Outputs map[string]*WorkflowCallEventOutput
	// Pos is a position in source.
	Pos *Pos
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
	// Pos is a position in source.
	Pos *Pos
}

// PermissionScope is struct for respective permission scope like "issues", "checks", ...
type PermissionScope struct {
	// Name is name of the scope.
	Name *String
	// Value is permission value of the scope.
	Value *String
}

// Permissions is set of permission configurations in workflow file.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#permissions
type Permissions struct {
	// All represents a permission value for all the scopes at once.
	All *String
	// Scopes is mappings from scope name to its permission configuration.
	Scopes map[string]*PermissionScope
	// Pos is a position in source.
	Pos *Pos
}

// CacheModeKind identifies the cache access granted to a workflow or job.
type CacheModeKind uint8

const (
	// CacheModeInvalid represents an invalid declaration, distinct from an omitted one.
	CacheModeInvalid CacheModeKind = iota
	// CacheModeNone prevents restores and saves.
	CacheModeNone
	// CacheModeRead allows restores only.
	CacheModeRead
	// CacheModeWrite allows restores and saves.
	CacheModeWrite
	// CacheModeWriteOnly allows saves only.
	CacheModeWriteOnly
)

// CacheMode is an explicit cache-mode declaration. An omitted declaration is a nil pointer.
type CacheMode struct {
	Kind CacheModeKind
	Pos  *Pos
}

// DefaultsRun is configuration that shell is how to be run.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#defaultsrun
type DefaultsRun struct {
	// Expression supplies the complete run defaults mapping.
	Expression *String
	// Shell is shell name to be run.
	Shell *String
	// WorkingDirectory is a default working directory path.
	WorkingDirectory *String
	// Pos is a position in source.
	Pos *Pos
}

// Defaults is set of default configurations to run shell.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#defaults
type Defaults struct {
	// Run is configuration of how to run shell.
	Run *DefaultsRun
	// Pos is a position in source.
	Pos *Pos
}

// Concurrency is a configuration of concurrency of the workflow.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#concurrency
type Concurrency struct {
	// Expression supplies a group String or complete concurrency mapping.
	Expression *String
	// Group is name of the concurrency group.
	Group *String
	// CancelInProgress is a flag that shows if canceling this workflow cancels other jobs in progress.
	CancelInProgress *Bool
	// Queue is the queue strategy for pending workflow runs.
	Queue *String
	// Pos is a position in source.
	Pos *Pos
}

// Environment is a configuration of environment.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idenvironment
type Environment struct {
	// Expression supplies an environment name or complete environment mapping.
	Expression *String
	// Name is a name of environment which the workflow uses.
	Name *String
	// URL is the URL mapped to 'environment_url' in the deployments API.
	URL *String
	// Deployment is whether GitHub should create a deployment for this job.
	Deployment *Bool
	// Pos is a position in source.
	Pos *Pos
}

// ExecKind is kind of how the step is executed.
type ExecKind uint8

const (
	// ExecKindAction is kind for step to run action.
	ExecKindAction ExecKind = iota
	// ExecKindRun is kind for step to run shell script.
	ExecKindRun
	// ExecKindWait is kind for step to wait for background steps ('wait' or 'wait-all').
	ExecKindWait
	// ExecKindCancel is kind for step to cancel background steps ('cancel').
	ExecKindCancel
	// ExecKindParallel is kind for step to run a group of steps in parallel ('parallel').
	ExecKindParallel
)

// Exec is an interface how the step is executed.
type Exec interface {
	// Kind returns kind of the step execution.
	Kind() ExecKind
}

// ExecRun is configuration how to run shell script at the step.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsrun
type ExecRun struct {
	// Run is script to run.
	Run *String
	// Shell represents optional 'shell' field. Nil means nothing specified.
	Shell *String
	// WorkingDirectory represents optional 'working-directory' field. Nil means nothing specified.
	WorkingDirectory *String
	// RunPos is position of 'run' section.
	RunPos *Pos
}

// Input is an input field for running an action.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepswith
type Input struct {
	// Name is a name of the input.
	Name *String
	// Value is a value of the input.
	Value *String
}

// ActionInputMapping is a literal or expression-valued action input mapping.
type ActionInputMapping struct {
	// Inputs contains named input values.
	Inputs     map[string]*Input
	Expression *String
	// Insertions contains nested mapping directives.
	Insertions []*ActionInputMapping
}

// ExecAction is configuration how to run action at the step.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstepsuses
type ExecAction struct {
	// Uses is nil when its value could not be parsed as a nonempty String.
	Uses *String
	// Inputs represents inputs to the action in 'with' section. Keys are in lower case.
	Inputs map[string]*Input
	// InputsExpression supplies the complete action inputs mapping.
	InputsExpression *String
	// InputInsertions supplies additional action inputs through mapping directives.
	InputInsertions []*ActionInputMapping
	// Entrypoint represents optional 'entrypoint' field in 'with' section.
	Entrypoint *String
	// Args represents optional 'args' field in 'with' section.
	Args *String

	// Constant mapping results must not be interpreted as expression source.
	EntrypointEvaluated  bool
	ArgsEvaluated        bool
	InputNameExpressions []*String
}

// ExecWait is configuration of a step that waits for background steps to complete.
type ExecWait struct {
	// Names is the list of background step IDs to wait for. It is nil when 'wait-all' is used.
	Names []*String
	// All is true when the step waits for all preceding background steps via 'wait-all'.
	All bool
	// AllPos is the position of the 'wait-all' field. It is nil when 'wait' is used.
	AllPos *Pos
}

// ExecCancel is configuration of a step that cancels a running background step.
type ExecCancel struct {
	// Name is the ID of the background step to cancel.
	Name *String
}

// ExecParallel is configuration of a step that runs a group of steps in parallel.
type ExecParallel struct {
	// Steps is the group of steps to run in parallel.
	Steps []*Step
}

// RawYAMLValueKind is kind of raw YAML values.
type RawYAMLValueKind int

const (
	// RawYAMLValueKindObject is kind for an object value of raw YAML value.
	RawYAMLValueKindObject = RawYAMLValueKind(4)
	// RawYAMLValueKindArray is kind for an array value of raw YAML value.
	RawYAMLValueKindArray = RawYAMLValueKind(2)
	// RawYAMLValueKindString is kind for a String value of raw YAML value.
	RawYAMLValueKindString = RawYAMLValueKind(8)
)

// RawYAMLValue is a value at matrix variation, including mappings and arrays.
type RawYAMLValue interface {
	// Kind returns kind of raw YAML value.
	Kind() RawYAMLValueKind
	// Equals returns if the other value is equal to the value.
	Equals(other RawYAMLValue) bool
	// Pos returns a copy of the start position of the value in the source file.
	Pos() *Pos
	// String returns String representation of the value.
	String() string
}

// RawYAMLObject is raw YAML mapping value.
type RawYAMLObject struct {
	// Props is map from property names to their values. Keys are in lower case.
	Props    map[string]RawYAMLValue
	Position *Pos
}

// RawYAMLArray is raw YAML sequence value.
type RawYAMLArray struct {
	// Elems is list of elements of the array value.
	Elems    []RawYAMLValue
	Position *Pos
}

// RawYAMLString is raw YAML scalar value.
type RawYAMLString struct {
	// Value is String representation of the scalar node.
	Value string
	// Tag is the YAML tag of the scalar node, such as "!!str" or "!!int".
	Tag      string
	Position *Pos
}

// MatrixRow is one row of matrix.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type MatrixRow struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	// Name is a name of matrix value.
	Name *String
	// Values is variations of values which the matrix value can take.
	Values []RawYAMLValue
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// MatrixAssign represents which value should be taken in the row of the matrix.
type MatrixAssign struct {
	// Key is a name of the matrix value.
	Key *String
	// Value is the value selected from values in row.
	Value RawYAMLValue
}

// MatrixCombination is combination of matrix value assignments to define one of matrix variations.
type MatrixCombination struct {
	Evaluated bool // Expression results are data and must not be evaluated again.
	Assigns   map[string]*MatrixAssign
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// MatrixCombinations is list of combinations of matrix assignments used for 'include' and 'exclude'.
type MatrixCombinations struct {
	// Name is the include/exclude key as written in source.
	Name         *String
	Evaluated    bool
	Combinations []*MatrixCombination
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
}

// Matrix is matrix variations configuration of a job.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategymatrix
type Matrix struct {
	// Insertions supplies additional axes or filters through mapping directives.
	Insertions []*Matrix
	// Rows stores mappings from name to values. Keys are in lower case.
	Rows map[string]*MatrixRow
	// Include is list of combinations of matrix values and additional values.
	Include *MatrixCombinations
	// Exclude is list of combinations of matrix values which should not be run.
	Exclude *MatrixCombinations
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Strategy is strategy configuration of how the job is run.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idstrategy
type Strategy struct {
	// Expression supplies the complete strategy mapping.
	Expression *String
	// Matrix is matrix of combinations of values. Each combination will run the job once.
	Matrix *Matrix
	// FailFast is flag to show if other jobs should stop when one job fails.
	FailFast *Bool
	// MaxParallel is how many jobs should be run at once.
	MaxParallel *Int
	// Pos is a position in source.
	Pos *Pos
}

// EnvVar represents key-value of environment variable setup.
type EnvVar struct {
	// Name is name of the environment variable.
	Name *String
	// Value is String value of the environment variable.
	Value *String
}

// Env represents set of environment variables.
type Env struct {
	// Vars is mapping from env var name to env var value.
	Vars map[string]*EnvVar
	// Insertions contains environment mappings or object expressions supplied by ${{ insert }} directives.
	Insertions []*Env
	// Expression is an expression String which contains ${{ ... }}. When it is set, Vars and
	// Insertions are nil.
	Expression *String
}

// Step is step configuration. Step runs one action or one shell script.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idsteps
type Step struct {
	ID   *String
	If   *String
	Name *String
	Exec Exec
	// DeclaredUses records uses even when a conflicting execution key invalidates it.
	DeclaredUses    *String
	Env             *Env
	ContinueOnError *Bool
	TimeoutMinutes  *Float
	// Background is 'background' field. When it is true, the step runs asynchronously.
	Background *Bool
	// Pos is a position in source.
	Pos *Pos
}

// Credentials is credentials configuration.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainercredentials
type Credentials struct {
	// Username is username for authentication.
	Username *String
	// Password is password for authentication.
	Password *String
	// Expression is a String when expression syntax ${{ }} is used for this section.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Container is configuration of how to run the container.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idcontainer
type Container struct {
	// Expression supplies an image String or complete container mapping.
	Expression *String
	// Image is specification of Docker image.
	Image *String
	// Credentials is credentials configuration of the Docker container.
	Credentials *Credentials
	// Env is environment variables setup in the container.
	Env *Env
	// Ports is list of port number mappings of the container.
	Ports []*String
	// PortsExpression supplies the complete ports sequence.
	PortsExpression *String
	// Volumes are list of volumes to be mounted to the container.
	Volumes []*String
	// VolumesExpression supplies the complete volumes sequence.
	VolumesExpression *String
	// Options is options String to run the container.
	Options *String
	// Command overrides Docker image's default command for service containers.
	Command *String
	// Entrypoint overrides Docker image's default entrypoint for service containers.
	Entrypoint *String
	// Pos is a position in source.
	Pos *Pos
}

// Service is configuration to run a service like database.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idservices
type Service struct {
	// Name is name of the service.
	Name *String
	// Container is configuration of container which runs the service.
	Container *Container
}

// Services is a mapping from service ID to its configuration.
type Services struct {
	// Value is a mapping from service ID to its service instances. Keys are in lower case.
	Value map[string]*Service
	// Expression is an expression assigned to the services mapping by ${{ }} placeholder.
	Expression *String
	// Pos is a position in source.
	Pos *Pos
}

// Output is output entry of the job.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idoutputs
type Output struct {
	// Name is name of output.
	Name *String
	// Value is value of output.
	Value *String
}

// Runner is struct for runner configuration.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_idruns-on
type Runner struct {
	// Expression supplies labels or a complete runner selection mapping.
	Expression *String
	// Labels is list label names to select a runner to run a job.
	Labels []*String
	// LabelsExpr is a String when expression syntax ${{ }} is used for this section.
	LabelsExpr *String
	// Group is a group of runners specified in runs-on: section. It is nil when no group is specified.
	Group *String
}

// WorkflowCallInput is a normal input for workflow call.
type WorkflowCallInput struct {
	// Name is a name of the input.
	Name *String
	// Value is a value of the input.
	Value *String
}

// WorkflowCallSecret is a secret input for workflow call.
type WorkflowCallSecret struct {
	// Name is a name of the secret.
	Name *String
	// Value is a value of the secret.
	Value *String
}

// WorkflowCall represents a workflow call at jobs.<job_id>.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobsjob_iduses
type WorkflowCall struct {
	// Uses is a workflow specification to be called. This field is mandatory.
	Uses *String
	// Inputs is a map from input name to input value at 'with:'. Keys are in lower case.
	Inputs map[string]*WorkflowCallInput
	// Secrets is a map from secret name to secret value at 'secrets:'. Keys are in lower case.
	Secrets map[string]*WorkflowCallSecret
	// InheritSecrets is true when 'secrets: inherit' is specified. In this case, Secrets must be empty.
	InheritSecrets bool
}

// Snapshot represents image snapshot at jobs.<job_id>.snapshot.
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#jobsjob_idsnapshot
type Snapshot struct {
	// Expression supplies an image name or complete snapshot mapping.
	Expression *String
	// ImageName is a name of the custom image.
	ImageName *String
	// Version is a version of the custom image.
	Version *String
	// If is a condition whether the custom image is used.
	If *String
}

// Job is configuration of how to run a job.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions#jobs
type Job struct {
	// ID is an ID of the job, which is key of job configuration objects.
	ID *String
	// Name is a name of job that user can specify freely.
	Name *String
	// Needs is list of job IDs which should be run before running this job.
	Needs []*String
	// RunsOn is runner configuration which run the job.
	RunsOn *Runner
	// Permissions is permission configuration for running the job.
	Permissions *Permissions
	// CacheMode overrides the workflow's cache access for this job. Nil means inherit.
	CacheMode *CacheMode
	// Environment is environment specification where the job runs.
	Environment *Environment
	// Concurrency is concurrency configuration on running the job.
	Concurrency *Concurrency
	// Outputs is map from output name to output specifications.
	Outputs map[string]*Output
	// Env is environment variables setup while running the job.
	Env *Env
	// Defaults is default configurations of how to run scripts.
	Defaults *Defaults
	// If is a condition whether this job should be run.
	If *String
	// Steps is list of steps to be run in the job.
	Steps []*Step
	// TimeoutMinutes is timeout value of running the job in minutes.
	TimeoutMinutes *Float
	// CancelTimeoutMinutes is the time allowed for cancellation cleanup in minutes.
	CancelTimeoutMinutes *Float
	// Strategy is strategy configuration of running the job.
	Strategy *Strategy
	// ContinueOnError is a flag to show if execution should continue on error.
	ContinueOnError *Bool
	// Container is container configuration to run the job.
	Container *Container
	// Services is a services configurations.
	Services *Services
	// WorkflowCall is a workflow call by 'uses:'.
	WorkflowCall *WorkflowCall
	// DeclaredUses records uses even when step-only keys invalidate the call.
	DeclaredUses *String
	// Snapshot is a custom image snapshot.
	Snapshot *Snapshot
	// Pos is a position in source.
	Pos *Pos
}

// Workflow is root of workflow syntax tree, which represents one workflow configuration file.
// https://docs.github.com/en/actions/learn-github-actions/workflow-syntax-for-github-actions
type Workflow struct {
	// Name is name of the workflow. Nil when it is not specified.
	Name *String
	// Description describes the workflow or reusable workflow.
	Description *String
	// RunName is the name of workflow runs. This field can be set dynamically using ${{ }}.
	RunName *String
	// On is list of events which can trigger this workflow.
	On []Event
	// Permissions is configuration of permissions of this workflow.
	Permissions *Permissions
	// CacheMode sets default cache access. Nil leaves the trigger-dependent default in effect.
	CacheMode *CacheMode
	// Env is a default set of environment variables while running this workflow.
	Env *Env
	// Defaults is default configuration of how to run scripts.
	Defaults *Defaults
	// Concurrency is concurrency configuration of entire workflow.
	Concurrency *Concurrency
	// Jobs is mappings from job ID to the job object. Keys are in lower case.
	Jobs map[string]*Job
	// Pos is the first workflow mapping key's position. It may be nil for
	// workflows constructed directly by API callers.
	Pos *Pos
}
