package actionlintarch

import "strings"

const module = "actionlint.kjanat.dev/v2"

func within(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }
func internal(name string) string     { return module + "/internal/" + name }

// Each domain excludes these packages from its dependency graph.
var dependencyBans = map[string][]string{
	"engine":               {"application", "builtin", "checks", "config", "tools", "analyzerhost", "workspace", "diagnosticpolicy", "documentparse", "report", "workflow/composition"},
	"documentparse":        {"engine", "application", "builtin", "checks", "tools", "analyzerhost", "workspace", "config", "diagnosticpolicy"},
	"checks":               {"engine", "application", "tools", "analyzerhost", "workspace", "diagnosticpolicy", "render"},
	"tools/execution":      {"engine", "application", "workspace", "builtin", "checks", "diagnosticpolicy", "render"},
	"workflow/composition": {"application", "workspace", "diagnosticpolicy", "tools/adapters", "tools/backend", "tools/ruff", "tools/shellcheck", "tools/selection", "render"},
	"expression":           {"workflow", "analyzerhost", "tools", "config", "engine", "application", "workspace", "render"},
	"semantics":            {"workflow", "action", "builtin", "engine", "config", "workspace", "analyzerhost", "tools", "render"},
	"workflow/invocation":  {"application", "engine", "workspace", "config", "tools", "plugins", "documentparse", "render"},
}

func pure(path string) bool {
	for _, name := range []string{"expression", "semantics", "workflow/invocation"} {
		if within(path, internal(name)) && !strings.HasSuffix(path, "/generate") {
			return true
		}
	}
	for _, name := range []string{"source", "diagnostic", "document", "syntax", "rule", "analyzer", "resolver", "runner"} {
		if path == module+"/"+name {
			return true
		}
	}
	return false
}

func forbiddenEffect(path string) bool {
	// Allow URL, address and header value operations.
	if path == "net/url" || path == "net/netip" || path == "net/textproto" {
		return false
	}
	for _, prefix := range []string{"os", "net", "syscall", "unsafe", "go/ast", "go/parser", "go.yaml.in", "gopkg.in/yaml.v3", "github.com/tetratelabs/wazero", "github.com/kjanat/wazero", "github.com/kjanat/wacogo", "github.com/spf13/cobra"} {
		if within(path, prefix) {
			return true
		}
	}
	return false
}

// These packages may construct or modify the listed values.
var constructors = map[string][]string{
	module + "/source.Reference":    {internal("sourceid"), internal("plugins/component"), internal("plugins/core")},
	module + "/analyzer.Invocation": {internal("workflow/composition"), internal("plugins/component"), internal("plugins/core")},
	module + "/analyzer.Runtime":    {internal("tools/execution"), internal("plugins/component"), internal("plugins/core")},
}

func contextBoundary(path string) bool {
	return within(path, internal("cli")) || within(path, internal("githubaction")) || within(path, module+"/cmd") || within(path, module+"/tools") || strings.HasSuffix(path, "/generate") || strings.HasSuffix(path, "/metadata") || strings.HasSuffix(path, "/check-docs") || strings.HasSuffix(path, "/check-readme") || within(path, internal("conformance/fetch"))
}
