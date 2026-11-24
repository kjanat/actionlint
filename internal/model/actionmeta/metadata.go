// Package actionmeta parses action manifests and provides their source positions, schema validation, expression availability and runtime metadata.
package actionmeta

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"actionlint.kjanat.dev/v2/internal/base/sourcetext"
	"actionlint.kjanat.dev/v2/internal/model/calls"

	"actionlint.kjanat.dev/v2/internal/model/workflows/parser"

	"actionlint.kjanat.dev/v2/internal/languages/yaml/codec"
)

type ExprString struct {
	Value    string              `json:"value"`
	Position sourcetext.Position `json:"position"`
}

type KeyValue struct {
	Name  string     `json:"name"`
	Value ExprString `json:"value"`
}

type CompositeStep struct {
	Position  sourcetext.Position `json:"position"`
	IsMapping bool                `json:"is_mapping"`
	Keys      []string            `json:"keys"`
	// If, Run, WorkingDirectory, StepName and Uses are nil when the key is absent or its value is not a string.
	If               *ExprString `json:"if"`
	Run              *ExprString `json:"run"`
	WorkingDirectory *ExprString `json:"working_directory"`
	StepName         *ExprString `json:"name"`
	With             []*KeyValue `json:"with"`
	Env              []*KeyValue `json:"env"`
	Shell            *ExprString `yaml:"-" json:"-"`
	ContinueOnError  *ExprString `yaml:"-" json:"-"`
	WithExpression   *ExprString `yaml:"-" json:"-"`
	EnvExpression    *ExprString `yaml:"-" json:"-"`
	ID               *ExprString `yaml:"-" json:"-"`
	Node             *codec.Node `yaml:"-" json:"-"`
	Uses             *string     `json:"uses"`
}

func yamlActionExprString(n *codec.Node) *ExprString {
	n = Node(n)
	if value := yamlExprString(n); value != nil {
		return value
	}
	if value := Scalar(n); value != nil {
		return &ExprString{Value: *value, Position: sourcetext.Position{Line: n.Line, Column: n.Column}}
	}
	return nil
}

func yamlExprString(n *codec.Node) *ExprString {
	for n.Kind == codec.AliasNode && n.Alias != nil && n.Alias.Kind != codec.AliasNode {
		n = n.Alias
	}
	if n.Kind == codec.ScalarNode && n.Tag == "!!str" {
		return &ExprString{Value: n.Value, Position: sourcetext.Position{Line: n.Line, Column: n.Column}}
	}
	return nil
}

type actionCompositeSteps []*CompositeStep

type Runs struct {
	Plugin         string               `yaml:"plugin" json:"plugin,omitempty"`
	Using          string               `yaml:"using" json:"using"`
	Main           string               `yaml:"main" json:"main"`
	Pre            string               `yaml:"pre" json:"pre"`
	PreIf          string               `yaml:"pre-if" json:"pre-if"`
	Post           string               `yaml:"post" json:"post"`
	PostIf         string               `yaml:"post-if" json:"post-if"`
	Steps          actionCompositeSteps `yaml:"steps" json:"steps"`
	Image          string               `yaml:"image" json:"image"`
	PreEntrypoint  string               `yaml:"pre-entrypoint" json:"pre-entrypoint"`
	Entrypoint     string               `yaml:"entrypoint" json:"entrypoint"`
	PostEntrypoint string               `yaml:"post-entrypoint" json:"post-entrypoint"`
	Args           []any                `yaml:"args" json:"args"`
	Env            map[string]any       `yaml:"env" json:"env"`
}

type Branding struct {
	Icon  string `yaml:"icon"`
	Color string `yaml:"color"`
}

func Key(n *codec.Node) string {
	if key := Scalar(n); key != nil {
		return *key
	}
	return n.Value
}

func Fields(node *codec.Node) *codec.Node {
	n := Node(node)
	if n.Kind != codec.MappingNode {
		return n
	}
	normalized := *n
	normalized.Content = append([]*codec.Node(nil), n.Content...)
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := *n.Content[i]
		value := Scalar(&key)
		key.Alias, key.Anchor = nil, ""
		if value != nil {
			key.Kind, key.Style, key.Tag, key.Value = codec.ScalarNode, 0, "!!str", strings.ToLower(*value)
			if key.Value == "deprecationmessage" {
				key.Value = "deprecationMessage"
			}
		}
		normalized.Content[i] = &key
	}
	return &normalized
}

type Metadata struct {
	manifest     *manifest
	SourceMapped bool         `yaml:"-" json:"-"`
	Directory    string       `yaml:"-" json:"-"`
	Filename     string       `yaml:"-" json:"-"`
	Root         *codec.Node  `yaml:"-" json:"-"`
	Name         string       `yaml:"name" json:"name"`
	Description  string       `yaml:"description" json:"-"`
	Inputs       calls.Inputs `yaml:"inputs" json:"inputs"`
	// Outputs omits each output's description.
	Outputs calls.Outputs `yaml:"outputs" json:"outputs"`
	// SkipOutputs makes the outputs object accept any property besides the typed ones.
	SkipOutputs   bool        `yaml:"-" json:"skip_outputs"`
	Runs          Runs        `yaml:"runs" json:"runs"`
	Branding      Branding    `yaml:"branding" json:"-"`
	InputDefaults []*KeyValue `yaml:"-" json:"-"`
}

func (md *Metadata) Dir() string {
	return md.Directory
}

func (md *Metadata) Path() string {
	if sourcetext.RemoteAddress(md.Directory) {
		return md.Directory + "/" + md.Filename
	}
	return filepath.Join(md.Directory, md.Filename)
}

func expectedMapping(where string, n *codec.Node) error {
	return fmt.Errorf("yaml: %s must be mapping node but %s node was found at line:%d, col:%d", where, parser.NodeKindName(n.Kind), n.Line, n.Column)
}

// ParseActionMetadata returns partially decoded metadata along with a decode error.
func ParseActionMetadata(path string, content []byte) (*Metadata, error) {
	metadata := &Metadata{}
	owned := bytes.Clone(content)
	root, err := codec.ParseYAML(owned)
	if err == nil && root.Kind != 0 {
		err = Decode(root, metadata)
		if admissionErr := parser.AdmitYAMLAliases(root); admissionErr != nil {
			if _, fatal := codec.FatalLoadError(err); fatal {
				err = codec.NewAdmissionError(err)
			} else {
				err = errors.Join(err, admissionErr)
			}
		}
	}
	metadata.manifest = &manifest{text: string(owned)}
	metadata.Directory, metadata.Filename = filepath.Dir(path), filepath.Base(path)
	if sourcetext.RemoteAddress(path) {
		metadata.Directory, metadata.Filename = remotePathParts(path)
	}
	return metadata, err
}

func remotePathParts(address string) (string, string) {
	return strings.TrimSuffix(address, "/"+path.Base(address)), path.Base(address)
}

func (md *Metadata) Text() (string, bool) {
	if md == nil || md.manifest == nil {
		return "", false
	}
	return md.manifest.text, true
}
