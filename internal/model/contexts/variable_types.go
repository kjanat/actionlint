package contexts

import "actionlint.kjanat.dev/v2/internal/languages/expressions/exprtypes"

var builtinSecretProps = map[string]exprtypes.Type{
	"github_token":         exprtypes.StringType(),
	"actions_step_debug":   exprtypes.StringType(),
	"actions_runner_debug": exprtypes.StringType(),
}

var BuiltinGlobalVariableTypes = map[string]exprtypes.Type{
	"github": exprtypes.NewNamedObjectType(map[string]exprtypes.Type{
		"action":                    exprtypes.StringType(),
		"action_path":               exprtypes.StringType(), // Composite actions only.
		"action_ref":                exprtypes.StringType(),
		"action_repository":         exprtypes.StringType(),
		"action_status":             exprtypes.StringType(), // Composite actions only.
		"actor":                     exprtypes.StringType(),
		"actor_id":                  exprtypes.StringType(),
		"api_url":                   exprtypes.StringType(),
		"artifact_cache_size_limit": exprtypes.NumberType(), // Missing from the context reference.
		"artifacts":                 exprtypes.StringType(),
		"artifacts_list":            exprtypes.StringType(),
		"base_ref":                  exprtypes.StringType(),
		"env":                       exprtypes.StringType(),
		"event":                     exprtypes.NewEmptyObjectType().Type(),
		"event_name":                exprtypes.StringType(),
		"event_path":                exprtypes.StringType(),
		"graphql_url":               exprtypes.StringType(),
		"head_ref":                  exprtypes.StringType(),
		"job":                       exprtypes.StringType(),
		"output":                    exprtypes.StringType(), // Runner-provided file command path missing from the context reference.
		"path":                      exprtypes.StringType(),
		"ref":                       exprtypes.StringType(),
		"ref_name":                  exprtypes.StringType(),
		"ref_protected":             exprtypes.BoolType(),
		"ref_type":                  exprtypes.StringType(),
		"repository":                exprtypes.StringType(),
		"repository_id":             exprtypes.StringType(),
		"repository_owner":          exprtypes.StringType(),
		"repository_owner_id":       exprtypes.StringType(),
		"repository_visibility":     exprtypes.StringType(), // Missing from the context reference and documented as an OIDC claim.
		"repositoryUrl":             exprtypes.StringType(),
		"retention_days":            exprtypes.StringType(),
		"run_attempt":               exprtypes.StringType(),
		"run_id":                    exprtypes.StringType(),
		"run_number":                exprtypes.StringType(),
		"secret_source":             exprtypes.StringType(),
		"server_url":                exprtypes.StringType(),
		"sha":                       exprtypes.StringType(),
		"state":                     exprtypes.StringType(), // Runner-provided file command path missing from the context reference.
		"step_summary":              exprtypes.StringType(), // Runner-provided file command path missing from the context reference.
		"token":                     exprtypes.StringType(),
		"triggering_actor":          exprtypes.StringType(),
		"workflow":                  exprtypes.StringType(),
		"workflow_ref":              exprtypes.StringType(),
		"workflow_sha":              exprtypes.StringType(),
		"workspace":                 exprtypes.StringType(),
	}).Type(),
	"env": exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
	"job": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"check_run_id": exprtypes.NumberType(),
		"container": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
			"id":      exprtypes.StringType(),
			"network": exprtypes.StringType(),
		}).Type(),
		"services": exprtypes.NewMapObjectType(
			exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
				"id":      exprtypes.StringType(),
				"network": exprtypes.StringType(),
				"ports":   exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
			}).Type(),
		).Type(),
		"status":              exprtypes.StringType(),
		"workflow_file_path":  exprtypes.StringType(),
		"workflow_ref":        exprtypes.StringType(),
		"workflow_repository": exprtypes.StringType(),
		"workflow_sha":        exprtypes.StringType(),
	}).Type(),
	"steps": exprtypes.NewEmptyStrictObjectType().Type(), // Updated contextually.
	"runner": exprtypes.NewStrictObjectType(map[string]exprtypes.Type{
		"name":        exprtypes.StringType(),
		"os":          exprtypes.StringType(),
		"arch":        exprtypes.StringType(),
		"temp":        exprtypes.StringType(),
		"tool_cache":  exprtypes.StringType(),
		"debug":       exprtypes.StringType(),
		"environment": exprtypes.StringType(), // https://github.com/github/docs/issues/32443
	}).Type(),
	"secrets": exprtypes.NewStrictObjectType(builtinSecretProps).WithMapped(exprtypes.StringType()).Type(),
	"strategy": exprtypes.NewObjectType(map[string]exprtypes.Type{
		"fail-fast":    exprtypes.BoolType(),
		"job-index":    exprtypes.NumberType(),
		"job-total":    exprtypes.NumberType(),
		"max-parallel": exprtypes.NumberType(),
	}).Type(),
	"matrix": exprtypes.NewEmptyStrictObjectType().Type(), // Updated contextually.
	"needs":  exprtypes.NewEmptyStrictObjectType().Type(), // Updated contextually.
	"inputs": exprtypes.NewEmptyStrictObjectType().Type(),
	"vars":   exprtypes.NewMapObjectType(exprtypes.StringType()).Type(),
}
