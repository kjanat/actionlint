package scriptengine

// TemplateEffectsContract identifies the semantics of template provenance filtering.
const TemplateEffectsContract = "kjanat:actionlint/template-effects@0.1.0"

// TemplateEffect describes how masking template expressions affected a diagnostic.
type TemplateEffect string

const (
	TemplateEffectNone               TemplateEffect = "none"
	TemplateEffectLineLength         TemplateEffect = "line-length"
	TemplateEffectCompleteReferences TemplateEffect = "complete-references"
	TemplateEffectOpaqueName         TemplateEffect = "opaque-name"
	TemplateEffectOpaqueParentheses  TemplateEffect = "opaque-parentheses"
	TemplateEffectStringContents     TemplateEffect = "string-contents"
	TemplateEffectDocstringSummary   TemplateEffect = "docstring-summary"
	TemplateEffectExportedName       TemplateEffect = "exported-name"
	TemplateEffectPercentConversion  TemplateEffect = "percent-conversion"
)

// Valid rejects new effects until their semantics are understood by the host.
func (e TemplateEffect) Valid() bool {
	switch e {
	case TemplateEffectNone, TemplateEffectLineLength, TemplateEffectCompleteReferences,
		TemplateEffectOpaqueName, TemplateEffectOpaqueParentheses, TemplateEffectStringContents,
		TemplateEffectDocstringSummary, TemplateEffectExportedName, TemplateEffectPercentConversion:
		return true
	default:
		return false
	}
}
