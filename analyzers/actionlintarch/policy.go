package actionlintarch

import (
	_ "embed"
	"encoding/json"
	"strings"
)

const module = "actionlint.kjanat.dev/v2"

//go:embed packages.json
var packageInventory []byte

func classified(path string) bool {
	var packages []string
	if err := json.Unmarshal(packageInventory, &packages); err != nil {
		panic(err)
	}
	path = strings.TrimSuffix(path, "_test")
	relative := strings.TrimPrefix(path, module+"/")
	if path == module {
		relative = "."
	}
	for _, p := range packages {
		if p == relative {
			return true
		}
	}
	// Accepted new owner; its children still require explicit classification.
	return relative == "internal/workflow/invocation"
}

func within(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }
func internal(name string) string     { return module + "/internal/" + name }

// Existing ownership tests remain independent checks. These rules cover entire
// subtrees and report the import chain that crosses a responsibility boundary.
var boundaries = map[string][]string{
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
	// URL/address/header parsing is value computation, not network access.
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

// Value conversion at a wire boundary is allowed. Semantic reconstruction in
// tool adapters is not. Empty values for absence/error returns are allowed.
var constructors = map[string][]string{
	module + "/source.Reference":    {internal("sourceid"), internal("plugins/component"), internal("plugins/core")},
	module + "/analyzer.Invocation": {internal("workflow/composition"), internal("plugins/component"), internal("plugins/core")},
	module + "/analyzer.Runtime":    {internal("tools/execution"), internal("plugins/component"), internal("plugins/core")},
}

func contextBoundary(path string) bool {
	return within(path, internal("cli")) || within(path, internal("githubaction")) || within(path, module+"/cmd") || within(path, module+"/tools") || strings.HasSuffix(path, "/generate") || strings.HasSuffix(path, "/metadata") || strings.HasSuffix(path, "/check-docs") || strings.HasSuffix(path, "/check-readme") || within(path, internal("conformance/fetch"))
}
