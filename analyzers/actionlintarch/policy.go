package actionlintarch

import "strings"

const module = "actionlint.kjanat.dev/v2"

func within(path, prefix string) bool { return path == prefix || strings.HasPrefix(path, prefix+"/") }
func internal(name string) string     { return module + "/internal/" + name }

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
