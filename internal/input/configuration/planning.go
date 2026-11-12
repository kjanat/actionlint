package config

// InvocationPlanningConfig supplies the configurations a referenced action
// could use before its path is known. A repository supplies its own baseline.
// Without a repository, only overrides can enable tools disabled by the caller.
// The returned value is for planning, not execution or result processing.
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
