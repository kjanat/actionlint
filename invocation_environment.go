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
		switch variable.Name.Value {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
			"GIT_CONFIG", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE":
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
