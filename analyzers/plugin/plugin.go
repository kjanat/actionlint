// Package plugin registers the architecture analyzer with golangci-lint.
package plugin

import (
	"actionlint.kjanat.dev/v2/analyzers/actionlintarch"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() { register.Plugin("actionlintarch", newPlugin) }

type plugin struct{}

func newPlugin(any) (register.LinterPlugin, error) { return plugin{}, nil }
func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{actionlintarch.Analyzer}, nil
}
func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
