package action

// ProblemCode identifies a manifest or action-execution problem independently of lint selection.
// Producers choose it when the problem is detected; messages are never parsed
// to recover a code.
type ProblemCode string

const (
	CodeCoercion                ProblemCode = "coercion"
	CodeMissingField            ProblemCode = "missing-field"
	CodeUnexpectedField         ProblemCode = "unexpected-field"
	CodeInvalidFieldType        ProblemCode = "invalid-field-type"
	CodeDuplicateField          ProblemCode = "duplicate-field"
	CodeNestingDepth            ProblemCode = "nesting-depth"
	CodeExpressionForbidden     ProblemCode = "expression-forbidden"
	CodeMissingFile             ProblemCode = "missing-file"
	CodeInvalidDockerfile       ProblemCode = "invalid-dockerfile"
	CodeInvalidStepID           ProblemCode = "invalid-step-id"
	CodeDuplicateStepID         ProblemCode = "duplicate-step-id"
	CodeInvalidStep             ProblemCode = "invalid-step"
	CodeConflictingStepKind     ProblemCode = "conflicting-step-kind"
	CodeMissingStepKind         ProblemCode = "missing-step-kind"
	CodeMissingShell            ProblemCode = "missing-shell"
	CodeInvalidReference        ProblemCode = "invalid-reference"
	CodeReusableWorkflow        ProblemCode = "reusable-workflow"
	CodeExpressionSyntax        ProblemCode = "expression-syntax"
	CodeExpressionContext       ProblemCode = "expression-context"
	CodeExpressionReference     ProblemCode = "expression-reference"
	CodeExpressionCall          ProblemCode = "expression-call"
	CodeExpressionType          ProblemCode = "expression-type"
	CodeJSONMemberCollision     ProblemCode = "json-member-collision"
	CodeRuntimeUnsupported      ProblemCode = "runtime-unsupported"
	CodeRuntimeRemoved          ProblemCode = "runtime-removed"
	CodeRuntimeDeprecated       ProblemCode = "runtime-deprecated"
	CodeBrandingIcon            ProblemCode = "invalid-branding-icon"
	CodeBrandingColor           ProblemCode = "invalid-branding-color"
	CodeEmptyDeprecationMessage ProblemCode = "empty-deprecation-message"
	CodeUnknownInput            ProblemCode = "unknown-input"
	CodeDeprecatedInput         ProblemCode = "deprecated-input"
	CodeRequiredInput           ProblemCode = "required-input"
	CodeYAMLAnchor              ProblemCode = "yaml-anchor"
	CodeYAMLAlias               ProblemCode = "yaml-alias"
	CodeYAMLTag                 ProblemCode = "yaml-tag"
	CodeMetadataParse           ProblemCode = "metadata-parse"
	CodeMetadataRead            ProblemCode = "metadata-read"
	CodeMetadataEmpty           ProblemCode = "metadata-empty"
)
