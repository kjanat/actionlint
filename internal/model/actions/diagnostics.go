package action

// DiagnosticCode identifies a specific problem within the action rule family.
// Producers choose it when the problem is detected; messages are never parsed
// to recover a code.
type DiagnosticCode string

const (
	CodeMissingField            DiagnosticCode = "missing-field"
	CodeUnexpectedField         DiagnosticCode = "unexpected-field"
	CodeInvalidFieldType        DiagnosticCode = "invalid-field-type"
	CodeDuplicateField          DiagnosticCode = "duplicate-field"
	CodeNestingDepth            DiagnosticCode = "nesting-depth"
	CodeExpressionForbidden     DiagnosticCode = "expression-forbidden"
	CodeMissingFile             DiagnosticCode = "missing-file"
	CodeInvalidDockerfile       DiagnosticCode = "invalid-dockerfile"
	CodeInvalidStepID           DiagnosticCode = "invalid-step-id"
	CodeDuplicateStepID         DiagnosticCode = "duplicate-step-id"
	CodeInvalidStep             DiagnosticCode = "invalid-step"
	CodeConflictingStepKind     DiagnosticCode = "conflicting-step-kind"
	CodeMissingStepKind         DiagnosticCode = "missing-step-kind"
	CodeMissingShell            DiagnosticCode = "missing-shell"
	CodeInvalidReference        DiagnosticCode = "invalid-reference"
	CodeReusableWorkflow        DiagnosticCode = "reusable-workflow"
	CodeExpressionSyntax        DiagnosticCode = "expression-syntax"
	CodeExpressionContext       DiagnosticCode = "expression-context"
	CodeExpressionReference     DiagnosticCode = "expression-reference"
	CodeExpressionCall          DiagnosticCode = "expression-call"
	CodeExpressionType          DiagnosticCode = "expression-type"
	CodeJSONMemberCollision     DiagnosticCode = "json-member-collision"
	CodeRuntimeUnsupported      DiagnosticCode = "runtime-unsupported"
	CodeRuntimeRemoved          DiagnosticCode = "runtime-removed"
	CodeRuntimeDeprecated       DiagnosticCode = "runtime-deprecated"
	CodeBrandingIcon            DiagnosticCode = "invalid-branding-icon"
	CodeBrandingColor           DiagnosticCode = "invalid-branding-color"
	CodeEmptyDeprecationMessage DiagnosticCode = "empty-deprecation-message"
	CodeUnknownInput            DiagnosticCode = "unknown-input"
	CodeDeprecatedInput         DiagnosticCode = "deprecated-input"
	CodeRequiredInput           DiagnosticCode = "required-input"
	CodeYAMLAnchor              DiagnosticCode = "yaml-anchor"
	CodeYAMLAlias               DiagnosticCode = "yaml-alias"
	CodeYAMLTag                 DiagnosticCode = "yaml-tag"
	CodeMetadataParse           DiagnosticCode = "metadata-parse"
	CodeMetadataRead            DiagnosticCode = "metadata-read"
	CodeMetadataEmpty           DiagnosticCode = "metadata-empty"
)
