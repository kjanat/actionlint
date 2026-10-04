package actionlint

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

// ActionOutline describes an action manifest without exposing its parser state.
type ActionOutline struct {
	Path        string                `json:"path"`
	Name        string                `json:"name,omitempty"`
	Description string                `json:"description,omitempty"`
	ParseStatus string                `json:"parse_status"`
	Inputs      []ActionInputOutline  `json:"inputs"`
	Outputs     []ActionOutputOutline `json:"outputs"`
	Runs        ActionRuns            `json:"runs"`
}

// ActionInputOutline preserves declared required/default values independently.
// A default does not erase an explicitly declared required: true.
type ActionInputOutline struct {
	ID          string              `json:"id"`
	Description string              `json:"description,omitempty"`
	Required    *bool               `json:"required,omitempty"`
	Default     *string             `json:"default,omitempty"`
	Start       *DiagnosticPosition `json:"start,omitempty"`
}

// ActionOutputOutline preserves an output declaration and optional expression.
type ActionOutputOutline struct {
	ID          string              `json:"id"`
	Description string              `json:"description,omitempty"`
	Value       *string             `json:"value,omitempty"`
	Start       *DiagnosticPosition `json:"start,omitempty"`
}

func (ActionOutline) documentOutline() {}

// DocumentPath returns the manifest path.
func (a ActionOutline) DocumentPath() string { return a.Path }

// WithPath returns a copy with a different manifest path.
func (a ActionOutline) WithPath(path string) DocumentOutline {
	a.Path = path
	return a
}

// MarshalJSON derives the document kind from the concrete type.
func (a ActionOutline) MarshalJSON() ([]byte, error) {
	type plain ActionOutline
	return json.Marshal(struct {
		Kind string `json:"kind"`
		plain
	}{"action", plain(a)})
}

// UnmarshalJSON restores the concrete execution-runtime variant.
func (a *ActionOutline) UnmarshalJSON(data []byte) error {
	type plain ActionOutline
	var value plain
	wire := struct {
		*plain
		Runs json.RawMessage `json:"runs"`
	}{plain: &value}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	runs, err := decodeActionRuns(wire.Runs)
	if err != nil {
		return err
	}
	value.Runs = runs
	*a = ActionOutline(value)
	return nil
}

// ParseActionOutline parses a standalone action manifest without reading files
// or running analyzers. Parse errors accompany any recovered declarations.
// A complete parse does not imply the action's referenced files exist or run.
func ParseActionOutline(path string, content []byte) (ActionOutline, error) {
	var metadata ActionMetadata
	err := yaml.Unmarshal(content, &metadata)
	metadata.dir, metadata.file, metadata.src = filepath.Dir(path), filepath.Base(path), content
	return parsedActionOutline(path, &metadata, err)
}

// actionOutline projects metadata already decoded by the local-action cache.
func actionOutline(path string, metadata *ActionMetadata, parseErr error) ActionOutline {
	outline, _ := parsedActionOutline(path, metadata, parseErr)
	return outline
}

func parsedActionOutline(path string, metadata *ActionMetadata, parseErr error) (ActionOutline, error) {
	outline := ActionOutline{Path: path, ParseStatus: "failed", Inputs: []ActionInputOutline{}, Outputs: []ActionOutputOutline{}, Runs: UnknownRuns{}}
	if metadata == nil || metadata.schemaRoot == nil {
		if parseErr == nil {
			parseErr = errors.New("action metadata is empty")
		}
		return outline, parseErr
	}
	fields := actionOutlineFields(metadata.schemaRoot)
	outline.Name, outline.Description = actionOutlineString(fields["name"]), actionOutlineString(fields["description"])
	outline.Inputs = actionInputOutlines(fields["inputs"])
	outline.Outputs = actionOutputOutlines(fields["outputs"])
	outline.Runs = actionRunsOutline(fields["runs"])

	// Schema validation needs the runtime even if an earlier decoding error
	// stopped populating ActionMetadata.Runs. The original YAML remains intact.
	validated := *metadata
	runs := actionOutlineFields(fields["runs"])
	validated.Runs.Using = actionOutlineString(runs["using"])
	validated.Runs.Plugin = actionOutlineString(runs["plugin"])
	rule := NewRuleAction(nil)
	rule.checkActionMetadataSchema(&validated)
	issues := []error{parseErr}
	for _, err := range rule.Errs() {
		issues = append(issues, err)
	}
	if _, unknown := outline.Runs.(UnknownRuns); unknown {
		if validated.Runs.Using == "" {
			issues = append(issues, errors.New("action metadata requires runs.using or runs.plugin"))
		} else {
			issues = append(issues, fmt.Errorf("unknown action runtime %q", validated.Runs.Using))
		}
	}
	err := errors.Join(issues...)
	outline.ParseStatus = "complete"
	if err != nil {
		outline.ParseStatus = "partial"
	}
	return outline, err
}

func actionOutlineFields(node *yaml.Node) map[string]*yaml.Node {
	fields := map[string]*yaml.Node{}
	node = actionSchemaNode(node)
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			fields[strings.ToLower(actionMetadataKey(node.Content[i]))] = node.Content[i+1]
		}
	}
	return fields
}

func actionOutlineString(node *yaml.Node) string {
	if value := actionSchemaScalar(node); value != nil {
		return *value
	}
	return ""
}

func actionInputOutlines(node *yaml.Node) []ActionInputOutline {
	inputs := []ActionInputOutline{}
	node = actionSchemaNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return inputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		input := ActionInputOutline{
			ID: actionMetadataKey(key), Description: actionOutlineString(fields["description"]),
			Default: actionSchemaScalar(fields["default"]), Start: outlinePosition(posAt(key)),
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
	node = actionSchemaNode(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return outputs
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		fields := actionOutlineFields(node.Content[i+1])
		outputs = append(outputs, ActionOutputOutline{
			ID: actionMetadataKey(key), Description: actionOutlineString(fields["description"]),
			Value: actionSchemaScalar(fields["value"]), Start: outlinePosition(posAt(key)),
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
		if sequence := actionSchemaNode(fields["args"]); sequence != nil && sequence.Kind == yaml.SequenceNode {
			for _, arg := range sequence.Content {
				if value := actionSchemaScalar(arg); value != nil {
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
	if _, known := ActionRuntimes[strings.ToLower(using)]; known && strings.HasPrefix(strings.ToLower(using), "node") {
		return JavaScriptRuns{
			Using: using, Main: actionOutlineString(fields["main"]), Pre: actionOutlineString(fields["pre"]), Post: actionOutlineString(fields["post"]),
			PreIf: actionOutlineString(fields["pre-if"]), PostIf: actionOutlineString(fields["post-if"]),
		}
	}
	return UnknownRuns{Using: using}
}

func actionStepOutlines(node *yaml.Node) []StepOutline {
	steps := []StepOutline{}
	node = actionSchemaNode(node)
	if node == nil || node.Kind != yaml.SequenceNode {
		return steps
	}
	for _, node := range node.Content {
		fields := actionOutlineFields(node)
		step := StepOutline{
			ID: actionOutlineString(fields["id"]), Name: actionOutlineString(fields["name"]),
			Start: outlinePosition(posAt(node)), Kind: "unknown", Uses: actionOutlineString(fields["uses"]),
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
