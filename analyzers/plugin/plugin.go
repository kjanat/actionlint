// Package plugin registers the architecture analyzer with golangci-lint.
package plugin

import (
	"fmt"

	"actionlint.kjanat.dev/v2/analyzers/actionlintarch"
	"actionlint.kjanat.dev/v2/analyzers/architecture"
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() { register.Plugin("actionlintarch", newPlugin) }

type plugin struct{ analyzer *analysis.Analyzer }

func newPlugin(settings any) (register.LinterPlugin, error) {
	config, err := register.DecodeSettings[struct {
		Contract string `json:"contract"`
	}](settings)
	if err != nil {
		return nil, err
	}
	if config.Contract == "" {
		return nil, fmt.Errorf("actionlintarch requires a contract path")
	}
	contract, err := architecture.Load(config.Contract)
	if err != nil {
		return nil, err
	}
	if contract.Module() != "actionlint.kjanat.dev/v2" {
		return nil, fmt.Errorf("actionlintarch contract has wrong module %q", contract.Module())
	}
	return plugin{analyzer: actionlintarch.New(contract)}, nil
}
func (p plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{p.analyzer}, nil
}
func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
