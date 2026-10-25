package ruff

import (
	"fmt"
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
)

func backendTemplateContract(backend analyzer.ScriptBackend) string {
	if declared, ok := backend.(interface{ TemplateContract() string }); ok {
		return declared.TemplateContract()
	}
	if described, ok := backend.(analyzer.DescribedScriptBackend); ok {
		return described.ScriptDescriptor().TemplateContract
	}
	return ""
}

func checkTemplateVersion(output string) error {
	fields := strings.Fields(output)
	if len(fields) >= 2 && fields[0] == "ruff" && fields[1] == "0.17.0" {
		return nil
	}
	return fmt.Errorf("ruff template-effects adapter requires reviewed version 0.17.0; got %q", strings.TrimSpace(output))
}

// nativeTemplateEffect is the reviewed Ruff 0.17.0 adapter for the versioned
// template-effects contract. Human-readable diagnostic messages are irrelevant.
// New Ruff versions must review this mapping and its source-range fixtures.
func nativeTemplateEffect(code string) analyzer.ScriptTemplateEffect {
	switch code {
	case "E501":
		return analyzer.TemplateEffectLineLength
	case "F401", "F811", "F841", "F842", "ARG001", "ARG002", "ARG003", "ARG004", "ARG005", "B007":
		return analyzer.TemplateEffectCompleteReferences
	case "F821", "B018":
		return analyzer.TemplateEffectOpaqueName
	case "UP034":
		return analyzer.TemplateEffectOpaqueParentheses
	case "F501":
		return analyzer.TemplateEffectPercentConversion
	case "F502", "F503", "F504", "F505", "F506", "F507", "F508", "F521", "F522", "F523", "F524", "F525", "F541":
		return analyzer.TemplateEffectStringContents
	case "D400", "D415":
		return analyzer.TemplateEffectDocstringSummary
	case "F822":
		return analyzer.TemplateEffectExportedName
	default:
		return analyzer.TemplateEffectNone
	}
}
