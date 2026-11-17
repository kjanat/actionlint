package outline

import (
	"strings"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/workflows/definition"
	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"
)

// FromAction projects decoded action metadata into a report outline.
func FromAction(path string, metadata *actionmeta.Metadata, parseErr error) Action {
	outline := Action{Path: path, ParseStatus: "failed", Inputs: []ActionInputOutline{}, Outputs: []ActionOutputOutline{}, Runs: UnknownRuns{}}
	if metadata == nil || metadata.Root == nil {
		return outline
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
	return outline
}

func actionOutlineFields(node *codec.Node) map[string]*codec.Node {
	fields := map[string]*codec.Node{}
	node = actionmeta.Node(node)
	if node != nil && node.Kind == codec.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			fields[strings.ToLower(actionmeta.Key(node.Content[i]))] = node.Content[i+1]
		}
	}
	return fields
}

func actionOutlineString(node *codec.Node) string {
	if value := actionmeta.Scalar(node); value != nil {
		return *value
	}
	return ""
}

func actionInputOutlines(node *codec.Node) []ActionInputOutline {
	inputs := []ActionInputOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != codec.MappingNode {
		return inputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		input := ActionInputOutline{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Default: actionmeta.Scalar(fields["default"]), Start: outlinePosition(parser.PosAt(key)),
		}
		if required := fields["required"]; required != nil {
			session, cursor, err := codec.Open(required)
			if err == nil {
				var value *bool
				if err := cursor.Decode(&value); err == nil {
					input.Required = value
				}
				session.Close()
			}
		}
		inputs = append(inputs, input)
	}
	return inputs
}

func actionOutputOutlines(node *codec.Node) []ActionOutputOutline {
	outputs := []ActionOutputOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != codec.MappingNode {
		return outputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		outputs = append(outputs, ActionOutputOutline{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Value: actionmeta.Scalar(fields["value"]), Start: outlinePosition(parser.PosAt(key)),
		})
	}
	return outputs
}

func actionRunsOutline(node *codec.Node) ActionRuns {
	fields := actionOutlineFields(node)
	using := actionOutlineString(fields["using"])
	switch strings.ToLower(using) {
	case "composite":
		return CompositeRuns{Steps: actionStepOutlines(fields["steps"])}
	case "docker":
		args := []string{}
		if sequence := actionmeta.Node(fields["args"]); sequence != nil && sequence.Kind == codec.SequenceNode {
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

func actionStepOutlines(node *codec.Node) []StepOutline {
	steps := []StepOutline{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != codec.SequenceNode {
		return steps
	}
	for _, node := range node.Content {
		fields := actionOutlineFields(node)
		step := StepOutline{
			ID: actionOutlineString(fields["id"]), Name: actionOutlineString(fields["name"]),
			Start: outlinePosition(parser.PosAt(node)), Kind: unknownStepKind, Uses: actionOutlineString(fields["uses"]),
		}
		_, hasRun := fields["run"]
		_, hasUses := fields["uses"]
		switch {
		case hasRun && !hasUses:
			step.Kind = definition.ExecKindRun.String()
		case hasUses && !hasRun:
			step.Kind = definition.ExecKindAction.String()
		}
		steps = append(steps, step)
	}
	return steps
}
