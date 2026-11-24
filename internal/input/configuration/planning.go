package configuration

// InvocationPlanningConfig returns a value for planning only.
func InvocationPlanningConfig(caller, project *Config, repositoryRoot string) *Config {
	if project == nil || len(project.Overrides) == 0 {
		return caller
	}
	if repositoryRoot != "" || caller == nil {
		return project
	}
	next := *caller
	next.Overrides = project.Overrides
	return &next
}
