package diagnosticpolicy

import (
	"maps"
	"path/filepath"

	diagmodel "actionlint.kjanat.dev/v2/diagnostic"
	"actionlint.kjanat.dev/v2/internal/rulecatalog"
	"actionlint.kjanat.dev/v2/internal/sourceid"
)

func CompleteDiagnostic(d *diagmodel.Diagnostic, content []byte, path string, unavailable bool) {
	if descriptor, ok := rulecatalog.Find(d.Rule); ok {
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
		ref := sourceid.New(path, content).Ref("", "document", "", "$")
		d.Reference = &ref
	}
	for i := range d.Fixes {
		f := &d.Fixes[i]
		f.Revisions = maps.Clone(f.Revisions)
		if f.Revisions == nil {
			f.Revisions = map[string]string{}
		}
		for _, edit := range f.Edits {
			if filepath.Clean(edit.Path) == filepath.Clean(d.Path) && f.Revisions[edit.Path] == "" {
				f.Revisions[edit.Path] = sourceid.Digest(content)
			}
		}
	}
}
