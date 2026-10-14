package actionlint

// NewRuleShellcheckWithRunner creates a rule backed by an embedded checker callback.
// runner receives ShellCheck CLI arguments and the prepared
// script, and returns json1 output or an execution error. It may be called
// concurrently and must return an error for unsupported arguments.
// It owns execution deadlines and cancellation. No host environment, rc files,
// or external source files are inherited by this rule.
//
// Add the returned rule using LinterOptions.OnRulesCreated. Shell selection,
// expression sanitization, source mapping and suppression use the normal rule.
func NewRuleShellcheckWithRunner(runner func(args []string, stdin string) ([]byte, error)) *RuleShellcheck {
	if runner == nil {
		panic("ShellCheck runner must not be nil")
	}
	rule := newRuleShellcheck(&externalCommand{
		exe: "shellcheck", runner: runner, env: []string{"SHELLCHECK_OPTS="},
	})
	externalSources := false
	rule.config = &ShellcheckSettings{Config: ShellcheckRCDisabled, ExternalSources: &externalSources}
	// The fixed virtual root avoids os.Getwd, which browsers do not implement.
	rule.paths.analysis = "/"
	return rule
}
