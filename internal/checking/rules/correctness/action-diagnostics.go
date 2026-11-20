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
	Explanation:     "Check file references whose location the declaring format defines. For action metadata, check runs.main, runs.pre, runs.post and a local Dockerfile relative to the action directory. Unknown file availability produces no finding. Container entrypoints resolve inside the image and are excluded. Generated files can be absent before the build; disable this rule for those manifests with a per-file override.",
	Help:            "Correct the referenced path, provide the file, or disable missing-file for manifests whose files are generated before execution.",
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
	Help:            "Supply the required input or use an action version with a suitable default.",
	DeclaredBy:      "action metadata traversal",
}

var DeprecatedInputRule = &define.Rule{
	Name:            "deprecated-input",
	Description:     "Report supplied action inputs marked as deprecated",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Follow the action's deprecation message and use its replacement input.",
	DeclaredBy:      "action metadata traversal",
}

var ActionRuntimeRule = &define.Rule{
	Name:            "action-runtime",
	Description:     "Validate action runtimes accepted by GitHub Actions runners",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Explanation:     "Report unsupported, removed or deprecated runs.using runtimes in action metadata and referenced action definitions. This checks the GitHub Actions runner's runtime contract. The runtime-lifecycle rule separately checks language release support for setup-node inputs.",
	Help:            "Select a runtime supported by current GitHub Actions runners or update the referenced action.",
	DeclaredBy:      "action metadata traversal",
}

var ActionBrandingRule = &define.Rule{
	Name:            "action-branding",
	Description:     "Validate action branding icons and colors",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Choose an icon and color supported by GitHub Actions metadata.",
	DeclaredBy:      "action metadata traversal",
}

var ActionInputDeprecationRule = &define.Rule{
	Name:            "action-input-deprecation",
	Description:     "Report empty deprecation messages in action input declarations",
	Category:        "correctness",
	Subcategory:     "actions",
	Recommended:     true,
	SupportsActions: true,
	Help:            "Explain how callers should replace the deprecated input, or remove deprecationMessage.",
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
	Help:            "Check access to the action metadata file and correct the reported read failure.",
	DeclaredBy:      "action metadata traversal",
}
