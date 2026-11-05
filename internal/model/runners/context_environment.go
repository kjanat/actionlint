package runner

import "strings"

// ContextEnvironment describes a runner-exported equivalent of a context value.
// It says nothing about assignments made later inside a user script.
type ContextEnvironment struct {
	name          string
	compositeOnly bool
}

// Name is the environment variable exported by the runner.
func (e ContextEnvironment) Name() string { return e.name }

// Available reports whether the binding exists in this kind of run step.
func (e ContextEnvironment) Available(composite bool) bool {
	return e.name != "" && (!e.compositeOnly || composite)
}

// ContextEnvironmentFor resolves a static, case-insensitive context reference.
// Only string values with equivalent environment representations are included.
// Source: actions/runner src/Runner.Worker/{GitHubContext,RunnerContext}.cs.
func ContextEnvironmentFor(reference string) ContextEnvironment {
	return contextEnvironments[strings.ToLower(reference)]
}

var contextEnvironments = map[string]ContextEnvironment{
	"github.action":              {name: "GITHUB_ACTION"},
	"github.action_path":         {name: "GITHUB_ACTION_PATH", compositeOnly: true},
	"github.action_ref":          {name: "GITHUB_ACTION_REF"},
	"github.action_repository":   {name: "GITHUB_ACTION_REPOSITORY"},
	"github.actor":               {name: "GITHUB_ACTOR"},
	"github.actor_id":            {name: "GITHUB_ACTOR_ID"},
	"github.api_url":             {name: "GITHUB_API_URL"},
	"github.base_ref":            {name: "GITHUB_BASE_REF"},
	"github.env":                 {name: "GITHUB_ENV"},
	"github.event_name":          {name: "GITHUB_EVENT_NAME"},
	"github.event_path":          {name: "GITHUB_EVENT_PATH"},
	"github.graphql_url":         {name: "GITHUB_GRAPHQL_URL"},
	"github.head_ref":            {name: "GITHUB_HEAD_REF"},
	"github.job":                 {name: "GITHUB_JOB"},
	"github.output":              {name: "GITHUB_OUTPUT"},
	"github.path":                {name: "GITHUB_PATH"},
	"github.ref":                 {name: "GITHUB_REF"},
	"github.ref_name":            {name: "GITHUB_REF_NAME"},
	"github.ref_type":            {name: "GITHUB_REF_TYPE"},
	"github.repository":          {name: "GITHUB_REPOSITORY"},
	"github.repository_id":       {name: "GITHUB_REPOSITORY_ID"},
	"github.repository_owner":    {name: "GITHUB_REPOSITORY_OWNER"},
	"github.repository_owner_id": {name: "GITHUB_REPOSITORY_OWNER_ID"},
	"github.retention_days":      {name: "GITHUB_RETENTION_DAYS"},
	"github.run_attempt":         {name: "GITHUB_RUN_ATTEMPT"},
	"github.run_id":              {name: "GITHUB_RUN_ID"},
	"github.run_number":          {name: "GITHUB_RUN_NUMBER"},
	"github.server_url":          {name: "GITHUB_SERVER_URL"},
	"github.sha":                 {name: "GITHUB_SHA"},
	"github.state":               {name: "GITHUB_STATE"},
	"github.step_summary":        {name: "GITHUB_STEP_SUMMARY"},
	"github.triggering_actor":    {name: "GITHUB_TRIGGERING_ACTOR"},
	"github.workflow":            {name: "GITHUB_WORKFLOW"},
	"github.workflow_ref":        {name: "GITHUB_WORKFLOW_REF"},
	"github.workflow_sha":        {name: "GITHUB_WORKFLOW_SHA"},
	"github.workspace":           {name: "GITHUB_WORKSPACE"},
	"runner.os":                  {name: "RUNNER_OS"},
	"runner.arch":                {name: "RUNNER_ARCH"},
	"runner.name":                {name: "RUNNER_NAME"},
	"runner.temp":                {name: "RUNNER_TEMP"},
	"runner.tool_cache":          {name: "RUNNER_TOOL_CACHE"},
	"runner.debug":               {name: "RUNNER_DEBUG"},
	"runner.environment":         {name: "RUNNER_ENVIRONMENT"},
}
