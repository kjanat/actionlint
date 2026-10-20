package documentparse

import workflowfacts "actionlint.kjanat.dev/internal/workflow/facts"

func References(path string, content []byte, document Document, sources map[string][]byte) map[string]workflowfacts.References {
	refs := map[string]workflowfacts.References{path: workflowfacts.Index(path, content, document.Facts(), document.Metadata != nil)}
	for filename, source := range sources {
		parsed := Action(filename, filename, source)
		if parsed.Metadata != nil && parsed.CanAnalyze() {
			refs[filename] = workflowfacts.Index(filename, source, parsed.Facts(), true)
		}
	}
	return refs
}
