package actionlint

// ruleDescriptor supplies metadata to both rule instances and frontend discovery.
type ruleDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	build       func(ruleContext) (Rule, error)
	enabled     func(ruleContext) bool
}
type ruleContext struct {
	path                 string
	config               *Config
	actions              *LocalActionsCache
	workflows            *LocalReusableWorkflowCache
	process              *concurrentProcess
	shellcheck, pyflakes string
}

func builtinRuleDescriptors() []ruleDescriptor {
	return []ruleDescriptor{
		{Name: "syntax-check", Description: "Checks for GitHub Actions workflow syntax", Category: "correctness"},
		{Name: "matrix", Description: "Checks for matrix combinations in \"matrix:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleMatrix(), nil }},
		{Name: "credentials", Description: "Checks for credentials in \"services:\" configuration", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleCredentials(), nil }},
		{Name: "shell-name", Description: "Checks for shell names used for scripts in \"run:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleShellName(), nil }},
		{Name: "runner-label", Description: "Checks for GitHub-hosted and preset self-hosted runner labels in \"runs-on:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleRunnerLabel(), nil }},
		{Name: "events", Description: "Checks for workflow trigger events at \"on:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleEvents(), nil }},
		{Name: "job-needs", Description: "Checks for job IDs in \"needs:\". Undefined IDs and cyclic dependencies are checked", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleJobNeeds(), nil }},
		{Name: "parallel-steps", Description: "Checks \"wait\"/\"cancel\" references to background steps and steps forbidden inside a \"parallel\" group", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleParallelSteps(), nil }},
		{Name: "action", Description: "Checks for popular actions released on GitHub, local actions, and action calls at \"uses:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleAction(c.actions), nil }},
		{Name: "env-var", Description: "Checks for environment variables configuration at \"env:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleEnvVar(), nil }},
		{Name: "id", Description: "Checks for duplication and naming convention of job/step IDs", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleID(), nil }},
		{Name: "glob", Description: "Checks for glob syntax used in branch names, tags, and paths", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleGlob(), nil }},
		{Name: "permissions", Description: "Checks for permissions configuration in \"permissions:\". Permission names and permission scopes are checked", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRulePermissions(), nil }},
		{Name: "workflow-call", Description: "Checks for reusable workflow calls. Inputs and outputs of called reusable workflow are checked", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleWorkflowCall(c.path, c.workflows), nil }},
		{Name: "expression", Description: "Syntax and semantics checks for expressions embedded with ${{ }} syntax", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleExpression(c.actions, c.workflows), nil }},
		{Name: "deprecated-commands", Description: "Checks for deprecated \"set-output\", \"save-state\", \"set-env\", and \"add-path\" commands at \"run:\"", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleDeprecatedCommands(), nil }},
		{Name: "if-cond", Description: "Checks for if: conditions which are always true/false", Category: "correctness", build: func(c ruleContext) (Rule, error) { return NewRuleIfCond(), nil }},
		{Name: "require-commit-hash", Description: "Checks that every \"uses:\" is pinned to a full-length commit SHA or an image digest", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequireCommitHash(), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresCommitHash() }},
		{Name: "require-job-timeout", Description: "Checks that every job sets \"timeout-minutes:\" within the configured bounds", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequireJobTimeout(c.config.RequiresJobTimeout()), nil }, enabled: func(c ruleContext) bool { return c.config.RequiresJobTimeout().Enabled() }},
		{Name: "require-permissions", Description: "Checks for an explicit permissions declaration at workflow or job scope", Category: "policy", build: func(c ruleContext) (Rule, error) {
			return NewRuleRequirePermissions(c.config.RequiresPermissions()), nil
		}, enabled: func(c ruleContext) bool { return c.config.RequiresPermissions().Enabled() }},
		{Name: "required-actions", Description: "Checks that the actions listed in the \"required-actions\" policy in actionlint.yaml are used", Category: "policy", build: func(c ruleContext) (Rule, error) { return NewRuleRequiredActions(), nil }, enabled: func(c ruleContext) bool { return len(c.config.RequiredActions()) > 0 }},
		{Name: "shellcheck", Description: "Checks for shell script sources in \"run:\" using shellcheck", Category: "external", build: func(c ruleContext) (Rule, error) { return NewRuleShellcheck(c.shellcheck, c.process) }, enabled: func(c ruleContext) bool { return c.shellcheck != "" }},
		{Name: "pyflakes", Description: "Checks for Python script when \"shell: python\" is configured using Pyflakes", Category: "external", build: func(c ruleContext) (Rule, error) { return NewRulePyflakes(c.pyflakes, c.process) }, enabled: func(c ruleContext) bool { return c.pyflakes != "" }},
	}
}
func builtinRuleBase(name string) RuleBase {
	for _, descriptor := range builtinRuleDescriptors() {
		if descriptor.Name == name {
			return NewRuleBase(descriptor.Name, descriptor.Description)
		}
	}
	panic("unregistered built-in rule: " + name)
}
