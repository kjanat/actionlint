package python

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/scriptengine"
)

type Masks struct {
	identifiers        map[sourcetext.Position]int
	parentheses        map[sourcetext.Position]sourcetext.Position
	quoted             map[sourcetext.Position]string
	lines              map[int]bool
	sourceLines        []string
	percentConversions map[sourcetext.Position]bool
}

// Accept decides by the finding's template effect alone.
func (m *Masks) Accept(d scriptengine.Diagnostic) bool {
	if d.TemplateEffect == scriptengine.TemplateEffectLineLength && m.lines[d.Location.Line] {
		return false
	}
	// Opaque values, quoted annotations and exports may hide references.
	if len(m.identifiers) > 0 && d.TemplateEffect == scriptengine.TemplateEffectCompleteReferences {
		return false
	}
	if m.dynamicQuotedValue(d) {
		return false
	}
	if width, ok := m.identifiers[d.Location]; ok && d.TemplateEffect == scriptengine.TemplateEffectOpaqueName &&
		d.EndLocation == (sourcetext.Position{Line: d.Location.Line, Column: d.Location.Column + width}) {
		return false
	}
	// UP034 can identify the wrapper added around an opaque value.
	if end, ok := m.parentheses[d.Location]; ok && d.TemplateEffect == scriptengine.TemplateEffectOpaqueParentheses && d.EndLocation == end {
		return false
	}
	return true
}

func (m *Masks) dynamicQuotedValue(d scriptengine.Diagnostic) bool {
	contents := d.TemplateEffect == scriptengine.TemplateEffectStringContents
	summary := d.TemplateEffect == scriptengine.TemplateEffectDocstringSummary
	export := d.TemplateEffect == scriptengine.TemplateEffectExportedName
	conversion := d.TemplateEffect == scriptengine.TemplateEffectPercentConversion
	if !contents && !summary && !export && !conversion {
		return false
	}
	for start, name := range m.quoted {
		end := sourcetext.Position{Line: start.Line, Column: start.Column + len(name)}
		if (d.Location.Line < start.Line || d.Location.Line == start.Line && d.Location.Column <= start.Column) &&
			(d.EndLocation.Line > end.Line || d.EndLocation.Line == end.Line && d.EndLocation.Column >= end.Column) &&
			(contents || summary && m.inFirstParagraph(d.Location.Line, start.Line) || export || conversion && m.percentConversions[start]) {
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
