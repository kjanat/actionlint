package policy

import (
	"path/filepath"

	"actionlint.kjanat.dev/v2/internal/input/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/sha256hex"
	"actionlint.kjanat.dev/v2/internal/results"
)

func (processor Processor) completeDiagnostic(d *results.Diagnostic, content []byte, path string, unavailable bool) {
	if descriptor, ok := processor.RuleSet.FindRule(d.Rule); ok {
		if d.Help == "" {
			d.Help = descriptor.Help
		}
		if d.Explanation == "" {
			d.Explanation = descriptor.Explanation
		}
		if d.DocumentationURL == "" {
			d.DocumentationURL = descriptor.DocumentationURL
		}
	}
	if unavailable {
		d.Reference, d.Fixes = nil, nil
		return
	}
	if d.Reference == nil {
		ref := sourcetext.NewReferenceIndex(path, content).Ref("", "document", "", "$")
		d.Reference = &ref
	}
	for i := range d.Fixes {
		f := &d.Fixes[i]
		if f.Revisions == nil {
			f.Revisions = map[string]string{}
		}
		for _, edit := range f.Edits {
			if filepath.Clean(edit.Path) == filepath.Clean(d.Path) && f.Revisions[edit.Path] == "" {
				f.Revisions[edit.Path] = sha256hex.Digest(content)
			}
		}
	}
}
