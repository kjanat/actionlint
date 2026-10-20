package localapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"actionlint.kjanat.dev/internal/config"
	"actionlint.kjanat.dev/internal/config/tree"
	"actionlint.kjanat.dev/internal/fsread"
	"actionlint.kjanat.dev/internal/pluginref"
	"github.com/pelletier/go-toml/v2/unstable"
	"go.yaml.in/yaml/v4"
)

const maxPluginManifestBytes = 4 << 20

// PluginManifest edits the selected config's plugin declarations, retaining
// inherited inputs so edits cannot silently use an obsolete dependency graph.
type PluginManifest struct {
	Path       string
	References []pluginref.Reference
	Declared   bool
	Exists     bool
	original   []byte
	snapshots  map[string][]byte
}

// OpenPluginManifest reads only plugin declarations. Plugin rule configuration
// cannot be validated until the plugins supplying those rules have been loaded.
func OpenPluginManifest(ctx context.Context, path string) (*PluginManifest, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	manifest := &PluginManifest{Path: absolute, snapshots: map[string][]byte{}}
	data, err := readPluginManifest(ctx, absolute)
	if errors.Is(err, os.ErrNotExist) {
		manifest.snapshots[absolute] = nil
		return manifest, nil
	}
	if err != nil {
		return nil, err
	}
	manifest.original, manifest.Exists = data, true
	loaded, err := tree.Load(absolute, func(name string) ([]byte, error) {
		name, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		content, err := readPluginManifest(ctx, name)
		if err == nil {
			manifest.snapshots[name] = content
		}
		return content, err
	}, func(_ string, node *yaml.Node) (*yaml.Node, error) {
		return node, validateManifestRoot(node)
	})
	if err != nil {
		return nil, err
	}
	manifest.original = manifest.snapshots[absolute]
	manifest.References, manifest.Declared, err = manifestReferences(loaded.Node)
	if err != nil {
		return nil, fmt.Errorf("plugins in %s: %w", absolute, err)
	}
	return manifest, nil
}

func manifestReferences(node *yaml.Node) ([]pluginref.Reference, bool, error) {
	if err := validateManifestRoot(node); err != nil {
		return nil, false, err
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == "plugins" {
			var declarations config.PluginDeclarations
			if err := declarations.UnmarshalYAML(node.Content[i+1]); err != nil {
				return nil, true, err
			}
			return []pluginref.Reference(declarations), true, nil
		}
	}
	return nil, false, nil
}

func validateManifestRoot(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return errors.New("config must be a mapping")
	}
	keys := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.ShortTag() != "!!str" && key.ShortTag() != "!!merge" {
			return errors.New("config keys must be strings")
		}
		if keys[key.Value] {
			return fmt.Errorf("duplicate config key %q", key.Value)
		}
		keys[key.Value] = true
	}
	return nil
}

func safePluginManifestPath(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin config symlink refused: %s", current)
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func readPluginManifest(ctx context.Context, path string) ([]byte, error) {
	if err := safePluginManifestPath(path); err != nil {
		return nil, err
	}
	reader := fsread.New(ctx, nil, nil)
	file, err := reader.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxPluginManifestBytes {
		return nil, errors.New("plugin config must be a regular file of at most 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPluginManifestBytes+1))
	if len(data) > maxPluginManifestBytes {
		return nil, errors.New("plugin config exceeds 4 MiB")
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Distinguish an existing empty document from an absent document.
	return append([]byte{}, data...), nil
}

// CheckUnchanged checks every inherited file as well as the edited leaf.
func (m *PluginManifest) CheckUnchanged(ctx context.Context) error {
	for path, original := range m.snapshots {
		current, err := readPluginManifest(ctx, path)
		if errors.Is(err, os.ErrNotExist) && original == nil {
			continue
		}
		if err != nil {
			return err
		}
		if original == nil || !bytes.Equal(current, original) {
			return fmt.Errorf("plugin config changed during operation (%s); retry", path)
		}
	}
	return nil
}

// OriginalBytes returns a copy of the selected config's original bytes.
func (m *PluginManifest) OriginalBytes() []byte {
	return slices.Clone(m.original)
}

// Save writes an explicit leaf-level list, including [] when removing the last
// plugin. Other root settings and inherited files remain unchanged.
func (m *PluginManifest) Save(ctx context.Context, refs []pluginref.Reference) (err error) {
	data, err := editPluginManifest(m.Path, m.original, refs)
	if err != nil {
		return err
	}
	if len(data) > maxPluginManifestBytes {
		return errors.New("plugin config exceeds 4 MiB")
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}
	guard := m.Path + ".writing"
	if err := os.Mkdir(guard, 0o700); err != nil {
		return fmt.Errorf("plugin config busy (%s): %w", guard, err)
	}
	defer func() { err = errors.Join(err, os.Remove(guard)) }()
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(m.Path), ".actionlint-config-*")
	if err != nil {
		return err
	}
	defer func() {
		if e := os.Remove(temp.Name()); !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, e)
		}
	}()
	if m.Exists {
		info, err := os.Stat(m.Path)
		if err != nil {
			return errors.Join(err, temp.Close())
		}
		if err := temp.Chmod(info.Mode().Perm()); err != nil {
			return errors.Join(err, temp.Close())
		}
	}
	if _, err := temp.Write(data); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := m.CheckUnchanged(ctx); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), m.Path); err != nil {
		return err
	}
	m.original = data
	m.snapshots[m.Path] = data
	m.References, m.Declared, m.Exists = slices.Clone(refs), true, true
	return nil
}

func pluginManifestSequence(refs []pluginref.Reference) (*yaml.Node, error) {
	sequence := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, ref := range refs {
		normalized, err := pluginref.New(ref.Name, ref.Version, ref.Registry)
		if err != nil {
			return nil, err
		}
		if normalized.Registry == pluginref.DefaultRegistry {
			sequence.Content = append(sequence.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: normalized.String()})
			continue
		}
		entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, pair := range [][2]string{{"name", normalized.Name}, {"version", normalized.Version}, {"registry", normalized.Registry}} {
			entry.Content = append(entry.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pair[0]},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: pair[1]})
		}
		sequence.Content = append(sequence.Content, entry)
	}
	return sequence, nil
}

func editPluginManifest(path string, original []byte, refs []pluginref.Reference) ([]byte, error) {
	node, err := tree.Decode(path, original)
	if err != nil {
		return nil, err
	}
	if err := validateManifestRoot(node); err != nil {
		return nil, err
	}
	sequence, err := pluginManifestSequence(refs)
	if err != nil {
		return nil, err
	}
	var edited []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		edited, err = editJSONPlugins(original, sequence)
	case ".toml":
		edited, err = editTOMLPlugins(original, sequence)
	default:
		edited, err = editYAMLPlugins(original, node, sequence)
	}
	if err != nil {
		return nil, err
	}
	updated, err := tree.Decode(path, edited)
	if err != nil {
		return nil, fmt.Errorf("cannot safely edit plugin config: %w", err)
	}
	before, err := tree.Expand(node, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	after, err := tree.Expand(updated, map[*yaml.Node]bool{})
	if err != nil {
		return nil, err
	}
	if !sameManifestSettings(withoutManifestPlugins(before), withoutManifestPlugins(after)) {
		return nil, errors.New("cannot edit plugins without changing other config settings")
	}
	actual, declared, err := manifestReferences(after)
	if err != nil {
		return nil, fmt.Errorf("invalid edited plugin declarations: %w", err)
	}
	if !declared {
		return nil, errors.New("edited config contains no plugin declarations")
	}
	expected, _, err := manifestReferences(&yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "plugins"}, sequence,
	}})
	if err != nil {
		return nil, err
	}
	if !slices.Equal(actual, expected) {
		return nil, errors.New("edited plugin declarations differ from requested plugins")
	}
	return edited, nil
}

func withoutManifestPlugins(node *yaml.Node) *yaml.Node {
	copyNode := *node
	copyNode.Content = nil
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value != "plugins" {
			copyNode.Content = append(copyNode.Content, node.Content[i], node.Content[i+1])
		}
	}
	return &copyNode
}

func sameManifestSettings(left, right *yaml.Node) bool {
	if left.Kind != right.Kind || left.ShortTag() != right.ShortTag() || left.Value != right.Value || len(left.Content) != len(right.Content) {
		return false
	}
	for i := range left.Content {
		if !sameManifestSettings(left.Content[i], right.Content[i]) {
			return false
		}
	}
	return true
}

func editYAMLPlugins(original []byte, root, sequence *yaml.Node) ([]byte, error) {
	if root.Style&yaml.FlowStyle != 0 {
		copyRoot := *root
		copyRoot.Content = slices.Clone(root.Content)
		root = &copyRoot
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "plugins" {
				root.Content[i+1] = sequence
				return yaml.Marshal(root)
			}
		}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugins"}, sequence)
		return yaml.Marshal(root)
	}
	replacement := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "plugins"}, sequence,
	}}
	start, end := len(original), len(original)
	lines := bytes.SplitAfter(original, []byte{'\n'})
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "plugins" {
			continue
		}
		replacement.Content[0].LineComment = key.LineComment
		sequence.LineComment = value.LineComment
		start = manifestLineOffset(lines, key.Line)
		if i+2 < len(root.Content) {
			end = manifestLineOffset(lines, root.Content[i+2].Line)
		}
		// Keep comments/blank lines introducing the next setting or footer.
		for end > start {
			lineStart := bytes.LastIndexByte(original[:max(start, end-1)], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' && !bytes.Equal(line, []byte("...")) {
				break
			}
			end = lineStart
		}
		break
	}
	data, err := yaml.Marshal(replacement)
	if err != nil {
		return nil, err
	}
	if bytes.Contains(original, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
	}
	if start == len(original) {
		// Append before an explicit document-end marker.
		for i, line := range lines {
			if bytes.Equal(bytes.TrimSpace(line), []byte("...")) {
				start = manifestLineOffset(lines, i+1)
				end = start
				break
			}
		}
		if start > 0 && original[start-1] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	return spliceManifest(original, start, end, data), nil
}

func manifestLineOffset(lines [][]byte, line int) int {
	offset := 0
	for i := 0; i < line-1 && i < len(lines); i++ {
		offset += len(lines[i])
	}
	return offset
}

func spliceManifest(original []byte, start, end int, replacement []byte) []byte {
	var result []byte
	result = append(result, original[:start]...)
	result = append(result, replacement...)
	return append(result, original[end:]...)
}

func manifestJSONValue(sequence *yaml.Node) ([]byte, error) {
	entries := make([]json.RawMessage, 0, len(sequence.Content))
	for _, entry := range sequence.Content {
		var data []byte
		var err error
		if entry.Kind == yaml.ScalarNode {
			data, err = json.Marshal(entry.Value)
		} else {
			mapping := map[string]string{}
			for i := 0; i < len(entry.Content); i += 2 {
				mapping[entry.Content[i].Value] = entry.Content[i+1].Value
			}
			data, err = json.Marshal(mapping)
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, data)
	}
	return json.Marshal(entries)
}

func editJSONPlugins(original []byte, sequence *yaml.Node) ([]byte, error) {
	value, err := manifestJSONValue(sequence)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(original)) == 0 {
		return append(append([]byte("{\n  \"plugins\": "), value...), []byte("\n}\n")...), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(original))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("JSON config must be an object")
	}
	count := 0
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		start := int(decoder.InputOffset())
		for start < len(original) && (original[start] == ':' || strings.ContainsRune(" \r\n\t", rune(original[start]))) {
			start++
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if key == "plugins" {
			return spliceManifest(original, start, int(decoder.InputOffset()), value), nil
		}
		count++
	}
	end := int(decoder.InputOffset())
	prefix := []byte("\n  \"plugins\": ")
	if count > 0 {
		prefix = append([]byte{','}, prefix...)
	}
	replacement := append(append(prefix, value...), '\n')
	return spliceManifest(original, end, end, replacement), nil
}

func editTOMLPlugins(original []byte, sequence *yaml.Node) ([]byte, error) {
	var values []string
	for _, entry := range sequence.Content {
		if entry.Kind == yaml.ScalarNode {
			value, err := json.Marshal(entry.Value)
			if err != nil {
				return nil, err
			}
			values = append(values, string(value))
		} else {
			var fields []string
			for i := 0; i < len(entry.Content); i += 2 {
				value, err := json.Marshal(entry.Content[i+1].Value)
				if err != nil {
					return nil, err
				}
				fields = append(fields, entry.Content[i].Value+" = "+string(value))
			}
			values = append(values, "{ "+strings.Join(fields, ", ")+" }")
		}
	}
	replacement := []byte("plugins = [" + strings.Join(values, ", ") + "]")
	parser := unstable.Parser{}
	parser.Reset(original)
	root := true
	pluginSection := -1
	var sections [][2]int
	for parser.NextExpression() {
		expression := parser.Expression()
		if expression.Kind == unstable.Table || expression.Kind == unstable.ArrayTable {
			root = false
			keys := expression.Key()
			if !keys.Next() {
				return nil, errors.New("TOML table has no key")
			}
			first := keys.Node()
			start := bytes.LastIndexByte(original[:int(first.Raw.Offset)], '\n') + 1
			if pluginSection >= 0 {
				sections = append(sections, [2]int{pluginSection, start})
				pluginSection = -1
			}
			if string(first.Data) == "plugins" {
				pluginSection = start
			}
		}
		if !root || expression.Kind != unstable.KeyValue {
			continue
		}
		keys := expression.Key()
		if keys.Next() && string(keys.Node().Data) == "plugins" && !keys.Next() {
			start := int(expression.Raw.Offset)
			return spliceManifest(original, start, start+int(expression.Raw.Length), replacement), nil
		}
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	if pluginSection >= 0 {
		sections = append(sections, [2]int{pluginSection, len(original)})
	}
	for _, section := range slices.Backward(sections) {
		end := section[1]
		for end > section[0] {
			lineStart := bytes.LastIndexByte(original[:end-1], '\n') + 1
			line := bytes.TrimSpace(original[lineStart:end])
			if len(line) != 0 && line[0] != '#' {
				break
			}
			end = lineStart
		}
		original = spliceManifest(original, section[0], end, nil)
	}
	// TOML root assignments must precede the first table header.
	return append(append(replacement, '\n'), original...), nil
}
