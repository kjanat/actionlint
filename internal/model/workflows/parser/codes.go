package parser

// Code names the kind of acceptance failure behind a syntax-check finding.
// The set is closed: each code is a reason GitHub's workflow reader rejects a
// document, and it becomes diagnostic.Diagnostic.Code.
type Code string

const (
	// CodeYAML marks input the YAML reader cannot load.
	CodeYAML Code = "yaml"
	// CodeBOM marks a leading byte order mark, which GitHub reads as part of the first key.
	CodeBOM Code = "bom"
	// CodeMultipleDocuments marks a second YAML document; GitHub reads exactly one.
	CodeMultipleDocuments Code = "multiple-documents"
	// CodeAlias marks a YAML anchor or alias GitHub's reader refuses.
	CodeAlias Code = "alias"
	// CodeFlowNull marks a compact flow-mapping key without a value, such as {key:}.
	CodeFlowNull Code = "flow-null"
	// CodeMergeKey marks the YAML merge key "<<".
	CodeMergeKey Code = "merge-key"
	// CodeUnknownTag marks a tag outside the core schema, or a non-string tag on a quoted or block scalar.
	CodeUnknownTag Code = "unknown-tag"
	// CodeTagValue marks a value its explicit core-schema tag does not accept.
	CodeTagValue Code = "tag-value"
	// CodeTypeMismatch marks a node of the wrong kind or type for its key.
	CodeTypeMismatch Code = "type-mismatch"
	// CodeExpressionForm marks plain text where a single ${{ }} expression or literal is required.
	CodeExpressionForm Code = "expression-form"
	// CodeInvalidValue marks a value outside the set or range a key accepts.
	CodeInvalidValue Code = "invalid-value"
	// CodeEmptyValue marks an empty value or section.
	CodeEmptyValue Code = "empty-value"
	// CodeUnknownKey marks a key that is not allowed where it appears.
	CodeUnknownKey Code = "unknown-key"
	// CodeDuplicateKey marks a key defined twice, compared case-insensitively.
	CodeDuplicateKey Code = "duplicate-key"
	// CodeMissingKey marks a required key or section that is absent.
	CodeMissingKey Code = "missing-key"
	// CodeConflictingKeys marks keys that cannot be combined.
	CodeConflictingKeys Code = "conflicting-keys"
)
