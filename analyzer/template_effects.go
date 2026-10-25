package analyzer

// TemplateEffectsContract identifies the semantics of template provenance filtering.
const TemplateEffectsContract = "kjanat:actionlint/template-effects@0.1.0"

type ScriptTemplateEffect string

const (
	TemplateEffectNone               ScriptTemplateEffect = "none"
	TemplateEffectLineLength         ScriptTemplateEffect = "line-length"
	TemplateEffectCompleteReferences ScriptTemplateEffect = "complete-references"
	TemplateEffectOpaqueName         ScriptTemplateEffect = "opaque-name"
	TemplateEffectOpaqueParentheses  ScriptTemplateEffect = "opaque-parentheses"
	TemplateEffectStringContents     ScriptTemplateEffect = "string-contents"
	TemplateEffectDocstringSummary   ScriptTemplateEffect = "docstring-summary"
	TemplateEffectExportedName       ScriptTemplateEffect = "exported-name"
	TemplateEffectPercentConversion  ScriptTemplateEffect = "percent-conversion"
)

// Valid rejects new effects until their semantics are understood by the host.
func (e ScriptTemplateEffect) Valid() bool {
	switch e {
	case TemplateEffectNone, TemplateEffectLineLength, TemplateEffectCompleteReferences,
		TemplateEffectOpaqueName, TemplateEffectOpaqueParentheses, TemplateEffectStringContents,
		TemplateEffectDocstringSummary, TemplateEffectExportedName, TemplateEffectPercentConversion:
		return true
	default:
		return false
	}
}
