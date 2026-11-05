package context

import exprtypes "actionlint.kjanat.dev/v2/internal/languages/expressions/types"

// builtinSecretProps are the secrets which GitHub Actions always provides to a workflow.
// ACTIONS_STEP_DEBUG and ACTIONS_RUNNER_DEBUG seem supplied from caller of the workflow (#130).
var builtinSecretProps = map[string]exprtypes.Type{
	"github_token":         exprtypes.StringType(),
	"actions_step_debug":   exprtypes.StringType(),
	"actions_runner_debug": exprtypes.StringType(),
}

// BuiltinGlobalVariableTypes defines types of all global variables. All context variables are
// documented at https://docs.github.com/en/actions/learn-github-actions/contexts
var BuiltinGlobalVariableTypes = map[string]exprtypes.Type{
	// https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#github-context
	// Agents: https://docs.github.com/api/article/body?pathname=/en/actions/reference/workflows-and-actions/contexts
	"github": exprtypes.NewNamedObjectType(map[string]exprtypes.Type{
		"action":                    exprtypes.StringType(),
		"action_path":               exprtypes.StringType(), // Note: Composite actions only
		"action_ref":                exprtypes.StringType(),
		"action_repository":         exprtypes.StringType(),
		"action_status":             exprtypes.StringType(), // Note: Composite actions only
		"actor":                     exprtypes.StringType(),
		"actor_id":                  exprtypes.StringType(),
		"api_url":                   exprtypes.StringType(),
		"artifact_cache_size_limit": exprtypes.NumberType(), // Note: Missing from the context reference
		"artifacts":                 exprtypes.StringType(),
		"artifacts_list":            exprtypes.StringType(),
		"base_ref":                  exprtypes.StringType(),
		"env":                       exprtypes.StringType(),
		"event":                     exprtypes.NewEmptyObjectType().Type(), // Note: Stricter type check for this payload would be possible
		"event_name":                exprtypes.StringType(),
		"event_path":                exprtypes.StringType(),
		"graphql_url":               exprtypes.StringType(),
		"head_ref":                  exprtypes.StringType(),
		"job":                       exprtypes.StringType(),
		"output":                    exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"path":                      exprtypes.StringType(),
		"ref":                       exprtypes.StringType(),
		"ref_name":                  exprtypes.StringType(),
		"ref_protected":             exprtypes.BoolType(),
		"ref_type":                  exprtypes.StringType(),
		"repository":                exprtypes.StringType(),
		"repository_id":             exprtypes.StringType(),
		"repository_owner":          exprtypes.StringType(),
		"repository_owner_id":       exprtypes.StringType(),
		"repository_visibility":     exprtypes.StringType(), // Note: Missing from the context reference; documented as an OIDC claim
		"repositoryUrl":             exprtypes.StringType(),
		"retention_days":            exprtypes.StringType(),
		"run_attempt":               exprtypes.StringType(),
		"run_id":                    exprtypes.StringType(),
		"run_number":                exprtypes.StringType(),
		"secret_source":             exprtypes.StringType(),
		"server_url":                exprtypes.StringType(),
		"sha":                       exprtypes.StringType(),
		"state":                     exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"step_summary":              exprtypes.StringType(), // Note: Runner-provided file command path missing from the context reference
		"token":                     exprtypes.StringType(),
		"triggering_actor":          exprtypes.StringType(),
		"workflow":                  exprtypes.StringType(),
		"workflow_ref":              exprtypes.StringType(),
		"workflow_sha":              exprtypes.StringType(),
		"workspace":                 exprtypes.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#env-context
	"env": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(), // env.<env_name>
	// https://docs.github.com/en/actions/learn-github-actions/contexts#job-context
	"job": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"check_run_id": exprtypes.NumberType(),
		"container": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
			"id":      exprtypes.StringType(),
			"network": exprtypes.StringType(),
		}).Type(),
		"services": exprtypes.NewMapObjectType(
			exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
				"id":      exprtypes.StringType(), // job.services.<service id>.id
				"network": exprtypes.StringType(),
				"ports":   exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
			}).Type(),
		).Type(),
		"status":              exprtypes.StringType(),
		"workflow_file_path":  exprtypes.StringType(), // https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#job-context
		"workflow_ref":        exprtypes.StringType(),
		"workflow_repository": exprtypes.StringType(),
		"workflow_sha":        exprtypes.StringType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#steps-context
	"steps": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#runner-context
	"runner": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"name":        exprtypes.StringType(),
		"os":          exprtypes.StringType(),
		"arch":        exprtypes.StringType(),
		"temp":        exprtypes.StringType(),
		"tool_cache":  exprtypes.StringType(),
		"debug":       exprtypes.StringType(),
		"environment": exprtypes.StringType(), // https://github.com/github/docs/issues/32443
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#secrets-context
	"secrets": exprtypes.NewStrictObjectType(builtinSecretProps).WithMapped(exprtypes.StringType()).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#strategy-context
	"strategy": exprtypes.NewObjectType(map[string]exprtypes.Type{
		"fail-fast":    exprtypes.BoolType(),
		"job-index":    exprtypes.NumberType(),
		"job-total":    exprtypes.NumberType(),
		"max-parallel": exprtypes.NumberType(),
	}).Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#matrix-context
	"matrix": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#needs-context
	"needs": exprtypes.NewEmptyStrictObjectType().Type(), // This value will be updated contextually
	// https://docs.github.com/en/actions/learn-github-actions/contexts#inputs-context
	// https://docs.github.com/en/actions/learn-github-actions/reusing-workflows
	"inputs": exprtypes.NewEmptyStrictObjectType().Type(),
	// https://docs.github.com/en/actions/learn-github-actions/contexts#vars-context
	"vars": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(), // vars.<var_name>
}
