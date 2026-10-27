package documentparse

import (
	"errors"
	"strings"

	documentmodel "actionlint.kjanat.dev/v2/document"

	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/yaml/codec"
	"actionlint.kjanat.dev/v2/internal/yaml/syntax"
)

// ParseActionOutline parses a standalone action manifest without reading files
// or running analyzers. Decoder errors accompany any recovered declarations.
// Complete means decoding succeeded. Validity and executability need separate checks.
func ParseActionOutline(path string, content []byte) (documentmodel.Action, error) {
	metadata, err := actionmeta.Parse(path, content)
	return parsedActionOutline(path, metadata, err)
}

// ActionOutline projects metadata already decoded by the local-action cache.
func ActionOutline(path string, metadata *actionmeta.Metadata, parseErr error) documentmodel.Action {
	outline, _ := parsedActionOutline(path, metadata, parseErr)
	return outline
}

func parsedActionOutline(path string, metadata *actionmeta.Metadata, parseErr error) (documentmodel.Action, error) {
	outline := documentmodel.Action{Path: path, ParseStatus: "failed", Inputs: []documentmodel.Input{}, Outputs: []documentmodel.Output{}, Runs: documentmodel.UnknownRuns{}}
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

func actionOutlineFields(node *syntax.Node) map[string]*syntax.Node {
	fields := map[string]*syntax.Node{}
	node = actionmeta.Node(node)
	if node != nil && node.Kind == syntax.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			fields[strings.ToLower(actionmeta.Key(node.Content[i]))] = node.Content[i+1]
		}
	}
	return fields
}

func actionOutlineString(node *syntax.Node) string {
	if value := actionmeta.Scalar(node); value != nil {
		return *value
	}
	return ""
}

func actionInputOutlines(node *syntax.Node) []documentmodel.Input {
	inputs := []documentmodel.Input{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != syntax.MappingNode {
		return inputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		input := documentmodel.Input{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Default: actionmeta.Scalar(fields["default"]), Start: outlinePosition(workflowlang.PosAt(key)),
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

func actionOutputOutlines(node *syntax.Node) []documentmodel.Output {
	outputs := []documentmodel.Output{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != syntax.MappingNode {
		return outputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		outputs = append(outputs, documentmodel.Output{
			ID: actionmeta.Key(key), Description: actionOutlineString(fields["description"]),
			Value: actionmeta.Scalar(fields["value"]), Start: outlinePosition(workflowlang.PosAt(key)),
		})
	}
	return outputs
}

func actionRunsOutline(node *syntax.Node) documentmodel.ActionRuns {
	fields := actionOutlineFields(node)
	using := actionOutlineString(fields["using"])
	switch strings.ToLower(using) {
	case "composite":
		return documentmodel.CompositeRuns{Steps: actionStepOutlines(fields["steps"])}
	case "docker":
		args := []string{}
		if sequence := actionmeta.Node(fields["args"]); sequence != nil && sequence.Kind == syntax.SequenceNode {
			for _, arg := range sequence.Content {
				if value := actionmeta.Scalar(arg); value != nil {
					args = append(args, *value)
				}
			}
		}
		return documentmodel.DockerRuns{
			Image: actionOutlineString(fields["image"]), Entrypoint: actionOutlineString(fields["entrypoint"]),
			PreEntrypoint: actionOutlineString(fields["pre-entrypoint"]), PostEntrypoint: actionOutlineString(fields["post-entrypoint"]), Args: args,
		}
	case "":
		if plugin := actionOutlineString(fields["plugin"]); plugin != "" {
			return documentmodel.PluginRuns{Plugin: plugin}
		}
	}
	if _, known := actionmeta.Runtimes[strings.ToLower(using)]; known && strings.HasPrefix(strings.ToLower(using), "node") {
		return documentmodel.JavaScriptRuns{
			Using: using, Main: actionOutlineString(fields["main"]), Pre: actionOutlineString(fields["pre"]), Post: actionOutlineString(fields["post"]),
			PreIf: actionOutlineString(fields["pre-if"]), PostIf: actionOutlineString(fields["post-if"]),
		}
	}
	return documentmodel.UnknownRuns{Using: using}
}

func actionStepOutlines(node *syntax.Node) []documentmodel.Step {
	steps := []documentmodel.Step{}
	node = actionmeta.Node(node)
	if node == nil || node.Kind != syntax.SequenceNode {
		return steps
	}
	for _, node := range node.Content {
		fields := actionOutlineFields(node)
		step := documentmodel.Step{
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
		step.Reference = documentmodel.ParseReference(step.Uses)
		steps = append(steps, step)
	}
	return steps
}
