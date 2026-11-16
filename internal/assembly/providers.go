package assembly

import (
	"cmp"
	"slices"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/scriptanalysis/contract"
	"actionlint.kjanat.dev/v2/internal/input/configuration/configreport"
	"actionlint.kjanat.dev/v2/internal/plugins/pluginselection"
	"actionlint.kjanat.dev/v2/internal/results/report"
)

// Reasons a selected plugin or tool provider did not take part in an analysis.
const (
	inactiveDisabledTool   = "disabled by --no-tool or disable-tools"
	inactiveDisabledConfig = "disabled in configuration"
)

// providerStatuses lists the plugins sources selected and the tool providers
// tools registers, with the reason each inactive one did not take part.
// A tool counts as inactive when analysis reported it unavailable or every
// configuration disables it.
func providerStatuses(sources pluginselection.Sources, tools []contract.Registration, result *report.Analysis) []report.ProviderStatus {
	var statuses []report.ProviderStatus
	pinned := map[string]bool{}
	if sources.Releases != nil {
		for _, pin := range sources.Releases.Plugins {
			source := pin.Source()
			pinned[source] = true
			_, onDemand := sources.LazySources[source]
			statuses = append(statuses, report.ProviderStatus{Kind: report.PluginProvider, Name: pin.Name, Version: pin.Version, Digest: pin.SHA256, OnDemand: onDemand})
		}
	}
	for _, path := range sources.Paths {
		if pinned[path] {
			continue
		}
		status := report.ProviderStatus{Kind: report.PluginProvider, Name: path}
		if artifact := sources.Artifacts[path]; artifact != nil {
			status.Digest = artifact.Info.SHA256
		}
		statuses = append(statuses, status)
	}
	unavailable := map[string]string{}
	if result != nil {
		for _, check := range result.IncompleteChecks {
			if check.Rule != "" && check.Reason != "" {
				unavailable[check.Rule] = check.Reason
			}
		}
	}
	listed := map[string]bool{}
	for _, registration := range tools {
		if listed[registration.Name] {
			continue
		}
		listed[registration.Name] = true
		status := report.ProviderStatus{Kind: report.ToolProvider, Name: registration.Name}
		switch {
		case slices.Contains(sources.DisabledTools, registration.Name):
			status.Inactive = inactiveDisabledTool
		case result != nil && toolDisabledInConfigurations(registration.Name, result.Configurations):
			status.Inactive = inactiveDisabledConfig
		case unavailable[registration.Name] != "":
			status.Inactive = unavailable[registration.Name]
		}
		statuses = append(statuses, status)
	}
	for _, id := range slices.Concat(sources.SkippedTools, sources.DisabledTools) {
		if !listed[id] {
			listed[id] = true
			statuses = append(statuses, report.ProviderStatus{Kind: report.PluginProvider, Name: id, Inactive: inactiveDisabledTool})
		}
	}
	slices.SortStableFunc(statuses, func(a, b report.ProviderStatus) int {
		return cmp.Or(strings.Compare(string(a.Kind), string(b.Kind)), strings.Compare(a.Name, b.Name))
	})
	return statuses
}

// toolDisabledInConfigurations reports whether every selected configuration
// sets tools.<name>.enabled to false. It is false without configurations.
func toolDisabledInConfigurations(name string, configurations []configreport.Report) bool {
	if len(configurations) == 0 {
		return false
	}
	for _, configuration := range configurations {
		value, _ := settingValue(configuration.Inspection.Config, "/tools/"+pointerToken(name)+"/enabled")
		if enabled, isBool := value.(bool); !isBool || enabled {
			return false
		}
	}
	return true
}
