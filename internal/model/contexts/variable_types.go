package contexts

import "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

// builtinSecretProps are the secrets which GitHub Actions always provides to a workflow.
// ACTIONS_STEP_DEBUG and ACTIONS_RUNNER_DEBUG seem supplied from caller of the workflow (#130).
var builtinSecretProps = map[string]types.Type{
	"github_token":         types.StringType(),
	"actions_step_debug":   types.StringType(),
	"actions_runner_debug": types.StringType(),
}

// BuiltinGlobalVariableTypes defines types of all global variables. All context variables are
// documented at https://docs.github.com/en/actions/learn-github-actions/contexts
var BuiltinGlobalVariableTypes = map[string]types.Type{
	// https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
	// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/contexts
	"github": types.NewNamedObjectType(map[string]types.Type{
		"action":                    types.StringType(),
		"action_path":               types.StringType(), // Note: Composite actions only
		"action_ref":                types.StringType(),
		"action_repository":         types.StringType(),
		"action_status":             types.StringType(), // Note: Composite actions only
		"actor":                     types.StringType(),
		"actor_id":                  types.StringType(),
		"api_url":                   types.StringType(),
		"artifact_cache_size_limit": types.NumberType(), // Note: Missing from the context reference
		"artifacts":                 types.StringType(),
		"artifacts_list":            types.StringType(),
		"base_ref":                  types.StringType(),
		"env":                       types.StringType(),
		"event":                     types.NewEmptyObjectType().Type(), // Note: Stricter type check for this payload would be possible
		"event_name":                types.StringType(),
		"event_path":                types.StringType(),
		"graphql_url":               types.StringType(),
		"head_ref":                  types.StringType(),
		"job":                       types.StringType(),
		"output":                    types.StringType(), // Note: Runner-provided file command path missing from the context reference
		"path":                      types.StringType(),
		"ref":                       types.StringType(),
		"ref_name":                  types.StringType(),
		"ref_protected":             types.BoolType(),
		"ref_type":                  types.StringType(),
		"repository":                types.StringType(),
		"repository_id":             types.StringType(),
		"repository_owner":          types.StringType(),
		"repository_owner_id":       types.StringType(),
		"repository_visibility":     types.StringType(), // Note: Missing from the context reference; documented as an OIDC claim
		"repositoryUrl":             types.StringType(),
		"retention_days":            types.StringType(),
		"run_attempt":               types.StringType(),
		"run_id":                    types.StringType(),
		"run_number":                types.StringType(),
		"secret_source":             types.StringType(),
		"server_url":                types.StringType(),
		"sha":                       types.StringType(),
		"state":                     types.StringType(), // Note: Runner-provided file command path missing from the context reference
		"step_summary":              types.StringType(), // Note: Runner-provided file command path missing from the context reference
		"token":                     types.StringType(),
		"triggering_actor":          types.StringType(),
		"workflow":                  types.StringType(),
		"workflow_ref":              types.StringType(),
		"workflow_sha":              types.StringType(),
		"workspace":                 types.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#env-context
	"env": types.NewMapObjectType(types.StringType()).Type(), // env.<env_name>
	// https://docs.github.com/en/actions/learn-github-actions/contexts#job-context
	"job": types.NewStrictObjectType(map[string]types.Type{
		"check_run_id": types.NumberType(),
		"container": types.NewStrictObjectType(map[string]types.Type{
			"id":      types.StringType(),
			"network": types.StringType(),
		}).Type(),
		"services": types.NewMapObjectType(
			types.NewStrictObjectType(map[string]types.Type{
				"id":      types.StringType(), // job.services.<service id>.id
				"network": types.StringType(),
				"ports":   types.NewMapObjectType(types.StringType()).Type(),
			}).Type(),
		).Type(),
		"status":              types.StringType(),
		"workflow_file_path":  types.StringType(), // https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#job-context
		"workflow_ref":        types.StringType(),
		"workflow_repository": types.StringType(),
		"workflow_sha":        types.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#steps-context
	"steps": types.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#runner-context
	"runner": types.NewStrictObjectType(map[string]types.Type{
		"name":        types.StringType(),
		"os":          types.StringType(),
		"arch":        types.StringType(),
		"temp":        types.StringType(),
		"tool_cache":  types.StringType(),
		"debug":       types.StringType(),
		"environment": types.StringType(), // https://github.com/github/docs/issues/32443
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#secrets-context
	"secrets": types.NewStrictObjectType(builtinSecretProps).WithMapped(types.StringType()).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#strategy-context
	"strategy": types.NewObjectType(map[string]types.Type{
		"fail-fast":    types.BoolType(),
		"job-index":    types.NumberType(),
		"job-total":    types.NumberType(),
		"max-parallel": types.NumberType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#matrix-context
	"matrix": types.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	"needs": types.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#inputs-context
	// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
	"inputs": types.NewEmptyStrictObjectType().Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#vars-context
	"vars": types.NewMapObjectType(types.StringType()).Type(), // vars.<var_name>
}
