// Package syntax exposes workflow syntax trees and parser diagnostics.
package syntax

import (
	"actionlint.kjanat.dev/v2/diagnostic"
	workflowparser "actionlint.kjanat.dev/v2/internal/workflow/parser"
)

// Parse returns a recovered syntax tree and all parser findings. A non-nil tree
// may accompany findings; parsing alone does not run workflow rules. The tree is
// owned by the caller.
func Parse(source []byte) (*Workflow, []diagnostic.Diagnostic) {
	tree, findings := workflowparser.ParseDocument(source)
	return fromWorkflow(tree), findings
}
