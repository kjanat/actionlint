package execution

import (
	"strings"

	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
)

// CheckoutEnvironmentUnknown reports settings that can make checkout differ from the local index.
func CheckoutEnvironmentUnknown(env *workflowlang.Env, platform runnerlang.Platform) bool {
	return EnvironmentContains(env, platform, func(name, value string, known bool) bool {
		if LoaderEnvironmentUnknown(name, value, known) {
			return true
		}
		switch name {
		case "PATH":
			// Checkout resolves Git through PATH, which may select a wrapper.
			return true
		case "HOME", "XDG_CONFIG_HOME", "NODE_OPTIONS", "GIT_SSH_COMMAND", "GIT_SSH", "GIT_ASKPASS", "SSH_ASKPASS",
			"GIT_PROXY_COMMAND", "GIT_TEMPLATE_DIR", "GIT_EXEC_PATH", "GIT_ALLOW_PROTOCOL":
			if !known || value != "" {
				return true
			}
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
			"GIT_CONFIG", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE":
			return true
		}
		return false
	})
}

// EnvironmentContains evaluates mapping expressions once and tests their literal names and values.
func EnvironmentContains(env *workflowlang.Env, platform runnerlang.Platform, matches func(string, string, bool) bool) bool {
	if env == nil {
		return false
	}
	check := func(name, value string, known bool) bool {
		if platform != runnerlang.MacOrLinux {
			name = strings.ToUpper(name)
		}
		return matches(name, value, known)
	}
	if env.Expression != nil {
		literal, known := workflowlang.WorkflowExpressionLiteral(env.Expression)
		object, isObject := literal.(map[string]any)
		if !known || !isObject {
			return true
		}
		for name, value := range object {
			text, scalar := workflowlang.WorkflowScalarString(value)
			if check(name, text, scalar) {
				return true
			}
		}
	}
	for _, variable := range env.Vars {
		name, known := EnvironmentLiteral(variable.Name)
		if !known {
			return true
		}
		value, known := EnvironmentLiteral(variable.Value)
		if check(name, value, known) {
			return true
		}
	}
	for _, insertion := range env.Insertions {
		if EnvironmentContains(insertion, platform, matches) {
			return true
		}
	}
	return false
}

func UbuntuRunner(runner *workflowlang.Runner) bool {
	if !runnerlang.Hosted(runner) {
		return false
	}
	for _, label := range runnerlang.Labels(runner) {
		if !strings.HasPrefix(strings.ToLower(label.Value), "ubuntu-") {
			return false
		}
	}
	return true
}

// LoaderEnvironmentUnknown reports settings that can run code or substitute libraries before a tool starts.
func LoaderEnvironmentUnknown(name, value string, known bool) bool {
	switch name {
	case "LD_PRELOAD", "LD_AUDIT", "DYLD_INSERT_LIBRARIES", "DYLD_IMAGE_SUFFIX", "DYLD_ROOT_PATH":
		return !known || value != ""
	case "LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH", "DYLD_FALLBACK_LIBRARY_PATH", "DYLD_FALLBACK_FRAMEWORK_PATH":
		// Empty search-path components may select the current directory.
		return true
	default:
		return false
	}
}

// EnvironmentLiteral returns the value of an environment entry, preserving expression results as data.
func EnvironmentLiteral(value *workflowlang.String) (string, bool) {
	if value == nil {
		return "", false
	}
	if !value.ContainsExpression() {
		return value.Value, true
	}
	literal, known := workflowlang.WorkflowExpressionLiteral(value)
	if !known {
		return "", false
	}
	return workflowlang.WorkflowScalarString(literal)
}

// ServicesMayChangeWorkspace reports service mounts or options that can expose
// the workspace even when the job itself does not run in a container.
func ServicesMayChangeWorkspace(services *workflowlang.Services) bool {
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

func MacOSRunner(runner *workflowlang.Runner) bool {
	labels := runnerlang.Labels(runner)
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

func ShellEnvironmentUnknown(env *workflowlang.Env) bool {
	return ShellEnvironmentUnknownForPlatform(env, runnerlang.MacOrLinux)
}

func ShellEnvironmentUnknownForPlatform(env *workflowlang.Env, platform runnerlang.Platform) bool {
	return EnvironmentContains(env, platform, func(name, value string, known bool) bool {
		if LoaderEnvironmentUnknown(name, value, known) {
			return true
		}
		if strings.HasPrefix(name, "BASH_FUNC_") && strings.HasSuffix(name, "%%") {
			return true
		}
		switch name {
		case "BASH_ENV", "ENV", "CDPATH", "SHELLOPTS", "BASHOPTS":
			if !known || value != "" {
				return true
			}
		}
		return false
	})
}

func ShellPathUnknown(env *workflowlang.Env) bool {
	return ShellPathUnknownForPlatform(env, runnerlang.MacOrLinux)
}

func ShellPathUnknownForPlatform(env *workflowlang.Env, platform runnerlang.Platform) bool {
	return EnvironmentContains(env, platform, func(name, _ string, _ bool) bool {
		return name == "PATH"
	})
}
