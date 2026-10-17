package engine

import (
	"errors"
	"strings"

	actionmeta "actionlint.kjanat.dev/internal/action"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/yaml"
)

// ParseActionOutline parses a standalone action manifest without reading files
// or running analyzers. Decoder errors accompany any recovered declarations.
// Complete means decoding succeeded. Validity and executability need separate checks.
func ParseActionOutline(path string, content []byte) (ActionOutline, error) {
	metadata, err := actionmeta.Parse(path, content)
	return parsedActionOutline(path, metadata, err)
}

// actionOutline projects metadata already decoded by the local-action cache.
func actionOutline(path string, metadata *actionmeta.Metadata, parseErr error) ActionOutline {
	outline, _ := parsedActionOutline(path, metadata, parseErr)
	return outline
}

func parsedActionOutline(path string, metadata *actionmeta.Metadata, parseErr error) (ActionOutline, error) {
	outline := ActionOutline{Path: path, ParseStatus: "failed", Inputs: []ActionInputOutline{}, Outputs: []ActionOutputOutline{}, Runs: UnknownRuns{}}
	if metadata == nil || metadata.Root == nil {
		if parseErr == nil {
			parseErr = errors.New("action metadata is empty")
		}
		return outline, parseErr
	}
	fields := actionOutlineFields(metadata.Root)
	outline.Name, outline.Description = actionOutlineString(fields["name"]), actionOutlineString(fields["description"])
	outline.Inputs = actionInputOutlines(fields["inputs"])
	outline.Outputs = actionOutputOutlines(fields["outputs"])
	outline.Runs = actionRunsOutline(fields["runs"])

	outline.ParseStatus = "complete"
	if parseErr != nil {
		outline.ParseStatus = "partial"
	}
	return outline, parseErr
}

func actionOutlineFields(node *yaml.Node) map[string]*yaml.Node {
	fields := map[string]*yaml.Node{}
	node = actionmeta.Node(node)
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			fields[strings.ToLower(actionmeta.Key(node.Content[i]))] = node.Content[i+1]
		}
	}
	return fields
}

func actionOutlineString(node *yaml.Node) string {
	if value := actionmeta.Scalar(node); value != nil {
		return *value
	}
	return ""
}

func actionInputOutlines(node *yaml.Node) []ActionInputOutline {
	inputs := []ActionInputOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return inputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		input := ActionInputOutline{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Default: actionmeta.Scalar(fields["default"]), Start: outlinePosition(workflowlang.PosAt(key)),
		}
		if required := fields["required"]; required != nil {
			var value *bool
			if err := required.Decode(&value); err == nil {
				input.Required = value
			}
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func actionOutputOutlines(node *yaml.Node) []ActionOutputOutline {
	outputs := []ActionOutputOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return outputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		outputs = append(outputs, ActionOutputOutline{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Value: actionmeta.Scalar(fields["value"]), Start: outlinePosition(workflowlang.PosAt(key)),
		})
	}
	return outputs
}

func actionRunsOutline(node *yaml.Node) ActionRuns {
	fields := actionOutlineFields(node)
	using := actionOutlineString(fields["using"])
	switch strings.ToLower(using) {
	case "composite":
		return CompositeRuns{Steps: actionStepOutlines(fields["steps"])}
	case "docker":
		args := []string{}
		if sequence := actionmeta.Node(fields["args"]); sequence != nil && sequence.Kind == yaml.SequenceNode {
			for _, arg := range sequence.Content {
				if value := actionmeta.Scalar(arg); value != nil {
					args = append(args, *value)
				}
			}
		}
		return DockerRuns{
			Image: actionOutlineString(fields["image"]), Entrypoint: actionOutlineString(fields["entrypoint"]),
			PreEntrypoint: actionOutlineString(fields["pre-entrypoint"]), PostEntrypoint: actionOutlineString(fields["post-entrypoint"]), Args: args,
		}
	case "":
		if plugin := actionOutlineString(fields["plugin"]); plugin != "" {
			return PluginRuns{Plugin: plugin}
		}
	}
	if _, known := actionmeta.Runtimes[strings.ToLower(using)]; known && strings.HasPrefix(strings.ToLower(using), "node") {
		return JavaScriptRuns{
			Using: using, Main: actionOutlineString(fields["main"]), Pre: actionOutlineString(fields["pre"]), Post: actionOutlineString(fields["post"]),
			PreIf: actionOutlineString(fields["pre-if"]), PostIf: actionOutlineString(fields["post-if"]),
		}
	}
	return UnknownRuns{Using: using}
}

func actionStepOutlines(node *yaml.Node) []StepOutline {
	steps := []StepOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return steps
	}
	for _, node := range node.Content {
		fields := actionOutlineFields(node)
		step := StepOutline{
			ID: actionOutlineString(fields["id"]), Name: actionOutlineString(fields["name"]),
			Start: outlinePosition(workflowlang.PosAt(node)), Kind: "unknown", Uses: actionOutlineString(fields["uses"]),
		}
		_, hasRun := fields["run"]
		_, hasUses := fields["uses"]
		switch {
		case hasRun && !hasUses:
			step.Kind = "run"
		case hasUses && !hasRun:
			step.Kind = "uses"
		}
		step.Reference = ParseUsesReference(step.Uses)
		steps = append(steps, step)
	}
	return steps
}
