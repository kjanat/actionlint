package parser

type Code string

const (
	CodeYAML Code = "yaml"
	// CodeBOM marks a leading byte order mark, which GitHub reads as part of the first key.
	CodeBOM Code = "bom"
	// CodeMultipleDocuments marks a second YAML document; GitHub reads exactly one.
	CodeMultipleDocuments Code = "multiple-documents"
	CodeAlias             Code = "alias"
	// CodeFlowNull marks a compact flow-mapping key without a value, such as {key:}.
	CodeFlowNull Code = "flow-null"
	CodeMergeKey Code = "merge-key"
	// CodeUnknownTag marks a tag outside the core schema, or a non-string tag on a quoted or block scalar.
	CodeUnknownTag     Code = "unknown-tag"
	CodeTagValue       Code = "tag-value"
	CodeTypeMismatch   Code = "type-mismatch"
	CodeExpressionForm Code = "expression-form"
	CodeInvalidValue   Code = "invalid-value"
	CodeEmptyValue     Code = "empty-value"
	CodeUnknownKey     Code = "unknown-key"
	// CodeDuplicateKey marks a key defined twice, compared case-insensitively.
	CodeDuplicateKey    Code = "duplicate-key"
	CodeMissingKey      Code = "missing-key"
	CodeConflictingKeys Code = "conflicting-keys"
)
