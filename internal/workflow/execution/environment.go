package execution

import (
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/mapping"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	runnerlang "actionlint.kjanat.dev/v2/internal/workflow/runner"
	"actionlint.kjanat.dev/v2/internal/workflow/value"
)

// CheckoutEnvironmentUnknown reports settings that can make checkout differ from the local index.
func CheckoutEnvironmentUnknown(env mapping.Mapping, platform runnerlang.Platform) bool {
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
func EnvironmentContains(env mapping.Mapping, platform runnerlang.Platform, matches func(string, string, bool) bool) bool {
	check := func(name, value string, known bool) bool {
		if platform != runnerlang.MacOrLinux {
			name = strings.ToUpper(name)
		}
		return matches(name, value, known)
	}
	if !env.Complete() {
		return true
	}
	for name, item := range env.Entries() {
		text, known := EnvironmentLiteral(item)
		if check(name, text, known) {
			return true
		}
	}
	return false
}

func UbuntuRunner(selection value.Value) bool {
	if !runnerlang.Hosted(selection) {
		return false
	}
	for _, label := range runnerlang.Labels(selection) {
		name, known := label.Text()
		if !known || !strings.HasPrefix(strings.ToLower(name), "ubuntu-") {
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
func EnvironmentLiteral(semantic value.Value) (string, bool) {
	return semantic.ScalarString()
}

// ServicesMayChangeWorkspace reports service mounts or options that can expose
// the workspace even when the job itself does not run in a container.
func ServicesMayChangeWorkspace(services workflowdecl.ServicesView) bool {
	if !services.Present() {
		return false
	}
	if services.Expression().Present() {
		return true
	}
	for _, service := range services.Value() {
		container := service.Container()
		if !container.Present() || container.Expression().Present() || container.VolumesExpression().Present() || len(slices.Collect(container.Volumes())) != 0 {
			return true
		}
		if options := container.Options(); options.Present() && options.Value() != "" {
			return true
		}
	}
	return false
}

func MacOSRunner(selection value.Value) bool {
	labels := runnerlang.Labels(selection)
	if len(labels) == 0 {
		return false
	}
	for _, label := range labels {
		text, known := label.Text()
		if !known {
			return false
		}
		name := strings.ToLower(text)
		if !strings.HasPrefix(name, "macos-") && !strings.HasPrefix(name, "xcode-") {
			return false
		}
	}
	return true
}

func ShellEnvironmentUnknown(env mapping.Mapping) bool {
	return ShellEnvironmentUnknownForPlatform(env, runnerlang.MacOrLinux)
}

func ShellEnvironmentUnknownForPlatform(env mapping.Mapping, platform runnerlang.Platform) bool {
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

func ShellPathUnknown(env mapping.Mapping) bool {
	return ShellPathUnknownForPlatform(env, runnerlang.MacOrLinux)
}

func ShellPathUnknownForPlatform(env mapping.Mapping, platform runnerlang.Platform) bool {
	return EnvironmentContains(env, platform, func(name, _ string, _ bool) bool {
		return name == "PATH"
	})
}
