package parsing

import "actionlint.kjanat.dev/v2/internal/model/workflows/facts"

// References preserves caller paths as lookup keys while identify supplies the
// stable document identity shared by aliases of the same source.
func References(path string, content []byte, document Document, sources map[string][]byte, identify func(string) string) map[string]facts.SourceIndex {
	refs := map[string]facts.SourceIndex{path: facts.Index(identify(path), content, document.Facts, document.Metadata != nil).SourceIndex()}
	for filename, source := range sources {
		parsed := Action(filename, source)
		if parsed.Metadata != nil && parsed.CanAnalyze() {
			refs[filename] = facts.Index(identify(filename), source, parsed.Facts, true).SourceIndex()
		}
	}
	return refs
}
