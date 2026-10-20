package documentparse

import workflowfacts "actionlint.kjanat.dev/v2/internal/workflow/facts"

// References preserves caller paths as lookup keys while identify supplies the
// stable document identity shared by aliases of the same source.
func References(path string, content []byte, document Document, sources map[string][]byte, identify func(string) string) map[string]workflowfacts.References {
	refs := map[string]workflowfacts.References{path: workflowfacts.Index(identify(path), content, document.Facts(), document.Metadata != nil)}
	for filename, source := range sources {
		parsed := Action(filename, filename, source)
		if parsed.Metadata != nil && parsed.CanAnalyze() {
			refs[filename] = workflowfacts.Index(identify(filename), source, parsed.Facts(), true)
		}
	}
	return refs
}
