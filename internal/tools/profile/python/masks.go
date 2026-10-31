package python

import (
	"strings"

	"actionlint.kjanat.dev/v2/analyzer"
)

// Masks records where sanitizing replaced templates with opaque values so
// findings those values manufacture can be filtered.
type Masks struct {
	identifiers        map[analyzer.ScriptPosition]int
	parentheses        map[analyzer.ScriptPosition]analyzer.ScriptPosition
	quoted             map[analyzer.ScriptPosition]string
	lines              map[int]bool
	sourceLines        []string
	percentConversions map[analyzer.ScriptPosition]bool
}

// Accept reports whether a finding is independent of the masks. Its template
// effect, never its code or message, decides.
func (m *Masks) Accept(d analyzer.ScriptDiagnostic) bool {
	if d.TemplateEffect == analyzer.TemplateEffectLineLength && m.lines[d.Location.Row] {
		return false
	}
	// Opaque values, quoted annotations and exports may hide references.
	// Comment-only templates add no masks; unrelated checks still apply.
	if len(m.identifiers) > 0 && d.TemplateEffect == analyzer.TemplateEffectCompleteReferences {
		return false
	}
	if m.dynamicQuotedValue(d) {
		return false
	}
	// A mask can manufacture undefined-name and useless-expression
	// findings. Only its exact identifier range is excluded.
	if width, ok := m.identifiers[d.Location]; ok && d.TemplateEffect == analyzer.TemplateEffectOpaqueName &&
		d.EndLocation == (analyzer.ScriptPosition{Row: d.Location.Row, Column: d.Location.Column + width}) {
		return false
	}
	// UP034 can identify the wrapper added around an opaque value.
	// Source parentheses extend outside this exact generated range.
	if end, ok := m.parentheses[d.Location]; ok && d.TemplateEffect == analyzer.TemplateEffectOpaqueParentheses && d.EndLocation == end {
		return false
	}
	return true
}

func (m *Masks) dynamicQuotedValue(d analyzer.ScriptDiagnostic) bool {
	// Filter content-dependent findings only when their range contains a quoted mask.
	contents := d.TemplateEffect == analyzer.TemplateEffectStringContents
	summary := d.TemplateEffect == analyzer.TemplateEffectDocstringSummary
	export := d.TemplateEffect == analyzer.TemplateEffectExportedName
	conversion := d.TemplateEffect == analyzer.TemplateEffectPercentConversion
	if !contents && !summary && !export && !conversion {
		return false
	}
	for start, name := range m.quoted {
		end := analyzer.ScriptPosition{Row: start.Row, Column: start.Column + len(name)}
		if (d.Location.Row < start.Row || d.Location.Row == start.Row && d.Location.Column <= start.Column) &&
			(d.EndLocation.Row > end.Row || d.EndLocation.Row == end.Row && d.EndLocation.Column >= end.Column) &&
			(contents || summary && m.inFirstParagraph(d.Location.Row, start.Row) || export || conversion && m.percentConversions[start]) {
			return true
		}
	}
	return false
}

func (m *Masks) inFirstParagraph(first, line int) bool {
	for row := first + 1; row < line; row++ {
		if row <= len(m.sourceLines) && strings.TrimSpace(m.sourceLines[row-1]) == "" {
			return false
		}
	}
	return true
}

func templateProvidesPercentConversion(prefix string) bool {
	index := strings.LastIndexByte(prefix, '%')
	if index < 0 {
		return false
	}
	count := 1
	for offset := index - 1; offset >= 0 && prefix[offset] == '%'; offset-- {
		count++
	}
	if count%2 == 0 {
		return false
	}
	for _, char := range prefix[index+1:] {
		if !strings.ContainsRune("#0- +.123456789*hlL", char) {
			return false
		}
	}
	return true
}
