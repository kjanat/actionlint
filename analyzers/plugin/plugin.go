// Package plugin registers the structural architecture checks with golangci-lint.
package plugin

import (
	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"fmt"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() { register.Plugin("actionlintarch", newPlugin) }

type plugin struct{}

func newPlugin(settings any) (register.LinterPlugin, error) {
	if settings != nil {
		return nil, fmt.Errorf("actionlintarch takes no settings or repository policy file")
	}
	return plugin{}, nil
}
func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{architecture.Analyzer}, nil
}
func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
