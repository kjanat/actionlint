package correctness

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/model/rule"
)

var MissingFileRule = &define.Rule{
	Scope:           rule.CompositeConfiguration,
	Name:            "missing-file",
	Description:     "Report referenced files confirmed missing from the inspected source",
	Category:        "correctness",
	Subcategory:     "files",
	Recommended:     true,
	SupportsActions: true,
	Explanation:     "Report missing files that action metadata references in runs.main, runs.pre, runs.post or a local Dockerfile. Paths resolve from the action directory.",
	Help:            "Fix the path or add the file. For files generated before the run, disable this rule in a per-file override.",
	DeclaredBy:      "action metadata traversal",
}

var UnknownInputRule = &define.Rule{
	Name:            "unknown-input",
	Description:     "Report action inputs absent from the available input declaration",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Use an input declared by the referenced action.",
	DeclaredBy:      "action metadata traversal",
}

var RequiredInputRule = &define.Rule{
	Name:            "required-input",
	Description:     "Report required action inputs missing from a known input mapping",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Supply the input, or use an action version that gives it a default.",
	DeclaredBy:      "action metadata traversal",
}

var DeprecatedInputRule = &define.Rule{
	Name:            "deprecated-input",
	Description:     "Report supplied action inputs marked as deprecated",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Follow the action's deprecation message.",
	DeclaredBy:      "action metadata traversal",
}

var ActionRuntimeRule = &define.Rule{
	Name:            "action-runtime",
	Description:     "Validate action runtimes accepted by GitHub Actions runners",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Explanation:     "Report unsupported, removed or deprecated runs.using values in action metadata and referenced actions.",
	Help:            "Use a supported runtime, or update the referenced action.",
	DeclaredBy:      "action metadata traversal",
}

var ActionBrandingRule = &define.Rule{
	Name:            "action-branding",
	Description:     "Validate action branding icons and colors",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Use a supported icon and color.",
	DeclaredBy:      "action metadata traversal",
}

var ActionInputDeprecationRule = &define.Rule{
	Name:            "action-input-deprecation",
	Description:     "Report empty deprecation messages in action input declarations",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Say what replaces the input, or remove deprecationMessage.",
	DeclaredBy:      "action metadata traversal",
}

var InvalidDockerfileRule = &define.Rule{
	Name:            "invalid-dockerfile",
	Description:     "Validate the filename of a Docker action's local image definition",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Point runs.image at a file named Dockerfile or use a supported registry image.",
	DeclaredBy:      "action metadata traversal",
}

var ActionMetadataReadRule = &define.Rule{
	Name:            "action-metadata-read",
	Description:     "Report action metadata that could not be read",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Check access to the metadata file and fix the reported error.",
	DeclaredBy:      "action metadata traversal",
}
