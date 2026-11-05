package correctness

import rule "actionlint.kjanat.dev/v2/internal/checking/rules"

// ParallelStepsDeclaration describes the parallel-steps rule.
func ParallelStepsDeclaration() rule.Declaration {
	return rule.Declaration{Name: "parallel-steps", Description: "Validate background-step references and restrictions within parallel groups", Category: "correctness", Subcategory: "steps", Maturity: "stable", Provider: "actionlint", Recommended: true, Explanation: "Validate background-step references and restrictions within parallel groups.", Help: "Check background-step names and wait/cancel references in the parallel group.", DocumentationURL: "https://github.com/kjanat/actionlint/blob/master/docs/rules.md#parallel-steps"}
}
