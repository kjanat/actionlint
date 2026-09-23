package actionlint

import "strings"

// Checkout inherits these settings; its files need not represent the local index.
func checkoutEnvironmentUnknown(env *Env) bool {
	if env == nil {
		return false
	}
	if env.Expression != nil {
		return true
	}
	for _, variable := range env.Vars {
		if loaderEnvironmentUnknown(variable) {
			return true
		}
		switch variable.Name.Value {
		case "NODE_OPTIONS":
			if variable.Value == nil || variable.Value.Value != "" {
				return true
			}
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
			"GIT_CONFIG", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE":
			return true
		}
	}
	return false
}

func ubuntuRunner(runner *Runner) bool {
	if !knownHostedRunner(runner) {
		return false
	}
	for _, label := range runnerPlatformLabels(runner) {
		if !strings.HasPrefix(strings.ToLower(label.Value), "ubuntu-") {
			return false
		}
	}
	return true
}

// A loader can run code or substitute libraries before the shell or Git starts.
func loaderEnvironmentUnknown(variable *EnvVar) bool {
	switch variable.Name.Value {
	case "LD_PRELOAD", "LD_AUDIT", "DYLD_INSERT_LIBRARIES", "DYLD_IMAGE_SUFFIX", "DYLD_ROOT_PATH":
		return variable.Value == nil || variable.Value.Value != ""
	case "LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH", "DYLD_FALLBACK_LIBRARY_PATH", "DYLD_FALLBACK_FRAMEWORK_PATH":
		// Empty search-path components may select the current directory.
		return true
	default:
		return false
	}
}

// Services run concurrently with every step. Opaque mounts/options can expose
// the workspace even when the job itself does not run in a container.
func servicesMayChangeWorkspace(services *Services) bool {
	if services == nil {
		return false
	}
	if services.Expression != nil {
		return true
	}
	for _, service := range services.Value {
		container := service.Container
		if container == nil || container.Expression != nil || container.VolumesExpression != nil || len(container.Volumes) != 0 ||
			container.Options != nil && container.Options.Value != "" {
			return true
		}
	}
	return false
}

func macOSRunner(runner *Runner) bool {
	labels := runnerPlatformLabels(runner)
	if len(labels) == 0 {
		return false
	}
	for _, label := range labels {
		name := strings.ToLower(label.Value)
		if !strings.HasPrefix(name, "macos-") && !strings.HasPrefix(name, "xcode-") {
			return false
		}
	}
	return true
}
